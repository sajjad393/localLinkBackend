package httpapi

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxAttachmentsPerMessage = 10
	defaultMaxFileSize       = int64(50 * 1024 * 1024)
	defaultDeviceQuota       = int64(1024 * 1024 * 1024)
	maxImagePixels           = int64(20_000_000)
)

type Attachment struct {
	ID           string `json:"id"`
	OriginalName string `json:"original_name"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	IsImage      bool   `json:"is_image"`
	DownloadURL  string `json:"download_url"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

type fileRecord struct {
	Attachment
	OwnerID       string
	StoragePath   string
	ThumbnailPath string
	CreatedAt     string
	Status        string
}

func (a *API) deviceStorageQuota() int64 {
	raw := strings.TrimSpace(os.Getenv("LOCALLINK_MAX_STORAGE_PER_DEVICE_MB"))
	if raw == "" {
		return defaultDeviceQuota
	}
	mb, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || mb < 1 || mb > math.MaxInt64/(1024*1024) {
		return defaultDeviceQuota
	}
	return mb * 1024 * 1024
}

func validClientFileID(value string) bool {
	for i, r := range value {
		if i == 0 && !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return len(value) >= 2 && len(value) <= 128
}

func (a *API) deviceStorageUsed(deviceID string) int64 {
	var used int64
	_ = a.store.DB().QueryRow(`SELECT COALESCE(SUM((CASE WHEN storage_size>0 THEN storage_size ELSE size END) + COALESCE(thumbnail_storage_size,0)),0) FROM files WHERE owner_id=? AND status='ready'`, deviceID).Scan(&used)
	return used
}

func (a *API) UploadFile(w http.ResponseWriter, r *http.Request) {
	select {
	case a.fileUploadGate <- struct{}{}:
		defer func() { <-a.fileUploadGate }()
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, "too many concurrent file transfers")
		return
	}
	owner, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	quota := a.deviceStorageQuota()
	if a.deviceStorageUsed(owner) >= quota {
		writeError(w, http.StatusRequestEntityTooLarge, "device storage quota exceeded")
		return
	}
	if r.ContentLength > a.maxFileSize+1<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.maxFileSize+1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form or file is too large")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	src, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer src.Close()

	safeName := filepath.Base(strings.TrimSpace(header.Filename))
	if safeName == "." || safeName == "" || strings.ContainsRune(safeName, '\x00') {
		safeName = "attachment"
	}
	if len(safeName) > 255 {
		safeName = safeName[:255]
	}

	tempFile, err := os.CreateTemp(a.fileRoot, ".upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create upload")
		return
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tempFile, hasher), io.LimitReader(src, a.maxFileSize+1))
	if err != nil {
		_ = tempFile.Close()
		writeError(w, http.StatusBadRequest, "failed to read upload")
		return
	}
	if written > a.maxFileSize {
		_ = tempFile.Close()
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large")
		return
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		writeError(w, http.StatusInternalServerError, "failed to finalize upload")
		return
	}
	if err := tempFile.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to close upload")
		return
	}

	sha := hex.EncodeToString(hasher.Sum(nil))
	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if normalized, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = normalized
	}
	if sniff, err := sniffContentType(tempPath); err == nil && sniff != "" && sniff != "application/octet-stream" {
		contentType = sniff
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	clientID := strings.TrimSpace(r.Header.Get("X-Client-File-ID"))
	if clientID != "" && (len(clientID) < 2 || len(clientID) > 128 || hasControlChars(clientID) || !validClientFileID(clientID)) {
		writeError(w, http.StatusBadRequest, "invalid client file id")
		return
	}

	// Client file IDs make offline/direct file retries idempotent. They are scoped
	// to the authenticated owner so one device cannot claim another device's ID.
	if clientID != "" {
		var existingOwner string
		var existingStatus string
		err = a.store.DB().QueryRow(`SELECT owner_id,status FROM files WHERE id=?`, clientID).Scan(&existingOwner, &existingStatus)
		if err == nil {
			if existingOwner != owner {
				writeError(w, http.StatusConflict, "client file id is already owned by another device")
				return
			}
			if existingStatus == "ready" {
				record, err := a.fileByID(clientID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "failed to read existing file")
					return
				}
				if record.Attachment.SHA256 != sha || record.Attachment.Size != written {
					writeError(w, http.StatusConflict, "client file id is already used for different content")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"file": record.Attachment, "deduplicated": true})
				return
			}
		} else if err != sql.ErrNoRows {
			writeError(w, http.StatusInternalServerError, "failed to check client file id")
			return
		}
	}

	var existingID string
	if clientID == "" {
		// Same owner + SHA-256 is safely reusable and avoids duplicate storage.
		err = a.store.DB().QueryRow(`SELECT id FROM files WHERE owner_id=? AND sha256=? AND status='ready' LIMIT 1`, owner, sha).Scan(&existingID)
		if err == nil {
			record, err := a.fileByID(existingID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read existing file")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"file": record.Attachment, "deduplicated": true})
			return
		}
		if err != sql.ErrNoRows {
			writeError(w, http.StatusInternalServerError, "failed to check duplicate file")
			return
		}
	}

	id := clientID
	if id == "" {
		id = uuid.NewString()
	}
	// The client id is a logical database key only. Physical storage names are
	// always server-generated so concurrent retries can never overwrite another
	// upload's file, even when they use the same client id.
	storageID := uuid.NewString()
	dir := filepath.Join(a.fileRoot, storageID[:2])
	if err := os.MkdirAll(dir, 0700); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create file directory")
		return
	}
	finalPath := filepath.Join(dir, storageID+".bin")
	if err := os.Rename(tempPath, finalPath); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store file")
		return
	}

	width, height, isImage := imageInfo(finalPath)
	thumbnailPath := ""
	var thumbnailStoredSize int64
	if isImage {
		thumbnailPath = filepath.Join(dir, storageID+"-thumb.png")
		if err := createThumbnail(finalPath, thumbnailPath, 320); err != nil {
			thumbnailPath = ""
		} else if info, err := os.Stat(thumbnailPath); err == nil && info.Mode().IsRegular() {
			thumbnailStoredSize = info.Size()
		} else {
			_ = os.Remove(thumbnailPath)
			thumbnailPath = ""
		}
	}

	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		_ = os.Remove(finalPath)
		if thumbnailPath != "" {
			_ = os.Remove(thumbnailPath)
		}
		writeError(w, http.StatusInternalServerError, "failed to start file metadata transaction")
		return
	}
	defer tx.Rollback()

	// Re-check identity, deduplication, and quota while holding the database
	// transaction. The earlier checks are only fast-fail hints; this transaction
	// is what prevents concurrent uploads from exceeding the per-device quota or
	// racing on a client-provided id.
	if clientID != "" {
		var existingOwner, existingStatus string
		err = tx.QueryRow(`SELECT owner_id,status FROM files WHERE id=?`, clientID).Scan(&existingOwner, &existingStatus)
		if err == nil {
			if existingOwner != owner {
				_ = tx.Rollback()
				_ = os.Remove(finalPath)
				if thumbnailPath != "" {
					_ = os.Remove(thumbnailPath)
				}
				writeError(w, http.StatusConflict, "client file id is already owned by another device")
				return
			}
			if existingStatus == "ready" {
				_ = tx.Rollback()
				_ = os.Remove(finalPath)
				if thumbnailPath != "" {
					_ = os.Remove(thumbnailPath)
				}
				record, ferr := a.fileByID(clientID)
				if ferr != nil || record.Attachment.SHA256 != sha || record.Attachment.Size != written {
					writeError(w, http.StatusConflict, "client file id is already used for different content")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"file": record.Attachment, "deduplicated": true})
				return
			}
		} else if err != sql.ErrNoRows {
			_ = tx.Rollback()
			_ = os.Remove(finalPath)
			if thumbnailPath != "" {
				_ = os.Remove(thumbnailPath)
			}
			writeError(w, http.StatusInternalServerError, "failed to check client file id")
			return
		}
	} else {
		var existingID string
		err = tx.QueryRow(`SELECT id FROM files WHERE owner_id=? AND sha256=? AND status='ready' LIMIT 1`, owner, sha).Scan(&existingID)
		if err == nil {
			_ = tx.Rollback()
			_ = os.Remove(finalPath)
			if thumbnailPath != "" {
				_ = os.Remove(thumbnailPath)
			}
			record, ferr := a.fileByID(existingID)
			if ferr != nil {
				writeError(w, http.StatusInternalServerError, "failed to read existing file")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"file": record.Attachment, "deduplicated": true})
			return
		}
		if err != sql.ErrNoRows {
			_ = tx.Rollback()
			_ = os.Remove(finalPath)
			if thumbnailPath != "" {
				_ = os.Remove(thumbnailPath)
			}
			writeError(w, http.StatusInternalServerError, "failed to check duplicate file")
			return
		}
	}

	var used int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM((CASE WHEN storage_size>0 THEN storage_size ELSE size END) + COALESCE(thumbnail_storage_size,0)),0) FROM files WHERE owner_id=? AND status='ready'`, owner).Scan(&used); err != nil {
		_ = os.Remove(finalPath)
		if thumbnailPath != "" {
			_ = os.Remove(thumbnailPath)
		}
		writeError(w, http.StatusInternalServerError, "failed to calculate storage quota")
		return
	}
	incomingStorage := written + thumbnailStoredSize
	if used < 0 || thumbnailStoredSize < 0 || incomingStorage < written || incomingStorage > quota || used > quota-incomingStorage {
		_ = os.Remove(finalPath)
		if thumbnailPath != "" {
			_ = os.Remove(thumbnailPath)
		}
		writeError(w, http.StatusRequestEntityTooLarge, "device storage quota exceeded")
		return
	}

	if _, err := tx.Exec(`
INSERT INTO files(id,owner_id,original_name,content_type,size,sha256,storage_path,thumbnail_path,width,height,is_image,created_at,status,storage_size,thumbnail_storage_size)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, owner, safeName, contentType, written, sha, finalPath, thumbnailPath, width, height, boolToInt(isImage), createdAt, "ready", written, thumbnailStoredSize); err != nil {
		_ = os.Remove(finalPath)
		if thumbnailPath != "" {
			_ = os.Remove(thumbnailPath)
		}
		writeError(w, http.StatusInternalServerError, "failed to save file metadata")
		return
	}
	if err := tx.Commit(); err != nil {
		_ = os.Remove(finalPath)
		if thumbnailPath != "" {
			_ = os.Remove(thumbnailPath)
		}
		writeError(w, http.StatusInternalServerError, "failed to commit file metadata")
		return
	}

	record, err := a.fileByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read uploaded file")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"file": record.Attachment, "deduplicated": false})
}

func (a *API) FileMetadata(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || !a.canReadFile(id, deviceID) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	record, err := a.fileByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	writeJSON(w, http.StatusOK, record.Attachment)
}

func (a *API) DownloadFile(w http.ResponseWriter, r *http.Request) {
	select {
	case a.fileDownloadGate <- struct{}{}:
		defer func() { <-a.fileDownloadGate }()
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, "too many concurrent file transfers")
		return
	}
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || !a.canReadFile(id, deviceID) {
		http.NotFound(w, r)
		return
	}
	record, err := a.fileByID(id)
	if err != nil || record.StoragePath == "" {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(record.StoragePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", record.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "bytes")
	kind := "attachment"
	if record.IsImage {
		kind = "inline"
	}
	w.Header().Set("Content-Disposition", contentDisposition(kind, record.OriginalName))
	http.ServeContent(w, r, record.OriginalName, fileModTime(record.StoragePath), file)
}

func (a *API) DownloadThumbnail(w http.ResponseWriter, r *http.Request) {
	select {
	case a.fileDownloadGate <- struct{}{}:
		defer func() { <-a.fileDownloadGate }()
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, "too many concurrent file transfers")
		return
	}
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || !a.canReadFile(id, deviceID) {
		http.NotFound(w, r)
		return
	}
	record, err := a.fileByID(id)
	if err != nil || record.ThumbnailPath == "" {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(record.ThumbnailPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", contentDisposition("inline", record.OriginalName+".png"))
	http.ServeContent(w, r, filepath.Base(record.ThumbnailPath), fileModTime(record.ThumbnailPath), file)
}

func (a *API) canReadFile(fileID, deviceID string) bool {
	var owner string
	if err := a.store.DB().QueryRow(`SELECT owner_id FROM files WHERE id=? AND status='ready'`, fileID).Scan(&owner); err != nil {
		return false
	}
	if owner == deviceID {
		return true
	}
	var n int
	if err := a.store.DB().QueryRow(`
SELECT 1 FROM message_attachments ma JOIN messages m ON m.id=ma.message_id
WHERE ma.file_id=? AND (m.sender_id=? OR m.recipient_id=?) LIMIT 1`, fileID, deviceID, deviceID).Scan(&n); err == nil {
		return true
	}
	if err := a.store.DB().QueryRow(`
SELECT 1 FROM group_message_attachments ga
JOIN group_messages gm ON gm.id=ga.group_message_id
JOIN group_members gmem ON gmem.group_id=gm.group_id
WHERE ga.file_id=? AND gmem.device_id=? LIMIT 1`, fileID, deviceID).Scan(&n); err == nil {
		return true
	}
	return false
}

func (a *API) fileByID(id string) (fileRecord, error) {
	var r fileRecord
	var imageFlag int
	var width, height sql.NullInt64
	err := a.store.DB().QueryRow(`
SELECT id,owner_id,original_name,content_type,size,sha256,COALESCE(width,0),COALESCE(height,0),is_image,storage_path,COALESCE(thumbnail_path,''),created_at,status
FROM files WHERE id=?`, id).Scan(
		&r.ID, &r.OwnerID, &r.OriginalName, &r.ContentType, &r.Size, &r.SHA256,
		&width, &height, &imageFlag, &r.StoragePath, &r.ThumbnailPath, &r.CreatedAt, &r.Status,
	)
	if err != nil {
		return r, err
	}
	if width.Valid {
		r.Width = int(width.Int64)
	}
	if height.Valid {
		r.Height = int(height.Int64)
	}
	r.IsImage = imageFlag != 0
	r.DownloadURL = "/api/v1/files/" + r.ID + "/content"
	if r.ThumbnailPath != "" {
		r.ThumbnailURL = "/api/v1/files/" + r.ID + "/thumbnail"
	}
	return r, nil
}

func (a *API) attachmentsForMessage(messageID string) ([]Attachment, error) {
	return a.attachmentsByQuery(`SELECT f.id,f.owner_id,f.original_name,f.content_type,f.size,f.sha256,COALESCE(f.width,0),COALESCE(f.height,0),f.is_image,f.storage_path,COALESCE(f.thumbnail_path,''),f.created_at,f.status
FROM message_attachments ma JOIN files f ON f.id=ma.file_id WHERE ma.message_id=? ORDER BY ma.position`, messageID)
}

func (a *API) attachmentsForGroupMessage(messageID string) ([]Attachment, error) {
	return a.attachmentsByQuery(`SELECT f.id,f.owner_id,f.original_name,f.content_type,f.size,f.sha256,COALESCE(f.width,0),COALESCE(f.height,0),f.is_image,f.storage_path,COALESCE(f.thumbnail_path,''),f.created_at,f.status
FROM group_message_attachments ma JOIN files f ON f.id=ma.file_id WHERE ma.group_message_id=? ORDER BY ma.position`, messageID)
}

func (a *API) attachmentsByQuery(query string, args ...any) ([]Attachment, error) {
	rows, err := a.store.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Attachment, 0)
	for rows.Next() {
		var r fileRecord
		var imageFlag int
		var width, height sql.NullInt64
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.OriginalName, &r.ContentType, &r.Size, &r.SHA256, &width, &height, &imageFlag, &r.StoragePath, &r.ThumbnailPath, &r.CreatedAt, &r.Status); err != nil {
			return nil, err
		}
		if width.Valid {
			r.Width = int(width.Int64)
		}
		if height.Valid {
			r.Height = int(height.Int64)
		}
		r.IsImage = imageFlag != 0
		r.DownloadURL = "/api/v1/files/" + r.ID + "/content"
		if r.ThumbnailPath != "" {
			r.ThumbnailURL = "/api/v1/files/" + r.ID + "/thumbnail"
		}
		if r.Status == "ready" {
			out = append(out, r.Attachment)
		}
	}
	return out, rows.Err()
}

func (a *API) validateAndAttachFiles(tx *sql.Tx, messageID string, group bool, ownerID string, ids []string) error {
	if len(ids) > maxAttachmentsPerMessage {
		return fmt.Errorf("too many attachments")
	}
	seen := make(map[string]struct{}, len(ids))
	for i, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("invalid attachment id")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate attachment id")
		}
		seen[id] = struct{}{}
		var fileOwner, status string
		if err := tx.QueryRow(`SELECT owner_id,status FROM files WHERE id=?`, id).Scan(&fileOwner, &status); err != nil {
			return fmt.Errorf("attachment not found")
		}
		if fileOwner != ownerID || status != "ready" {
			return fmt.Errorf("attachment is not owned by sender")
		}
		if group {
			_, err := tx.Exec(`INSERT INTO group_message_attachments(group_message_id,file_id,position) VALUES(?,?,?)`, messageID, id, i)
			if err != nil {
				return err
			}
		} else {
			_, err := tx.Exec(`INSERT INTO message_attachments(message_id,file_id,position) VALUES(?,?,?)`, messageID, id, i)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func imageInfo(path string) (int, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, false
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, false
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

func createThumbnail(srcPath, dstPath string, maxDim int) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scale := float64(maxDim) / float64(width)
	if height > width && float64(height) > float64(maxDim) {
		scale = float64(maxDim) / float64(height)
	}
	if scale > 1 {
		scale = 1
	}
	nw, nh := int(float64(width)*scale), int(float64(height)*scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := bounds.Min.Y + y*height/nh
		for x := 0; x < nw; x++ {
			sx := bounds.Min.X + x*width/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, dst)
}

func sniffContentType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	if n == 0 {
		return "application/octet-stream", nil
	}
	return http.DetectContentType(buf[:n]), nil
}

func (a *API) CleanupOrphanFiles(gracePeriod time.Duration) (int, error) {
	if gracePeriod < time.Hour {
		gracePeriod = time.Hour
	}
	cutoff := time.Now().UTC().Add(-gracePeriod).Format(time.RFC3339Nano)
	rows, err := a.store.DB().Query(`
SELECT f.id,f.storage_path,COALESCE(f.thumbnail_path,'')
FROM files f
WHERE f.status='ready' AND f.created_at < ?
  AND NOT EXISTS (SELECT 1 FROM message_attachments ma WHERE ma.file_id=f.id)
  AND NOT EXISTS (SELECT 1 FROM group_message_attachments ga WHERE ga.file_id=f.id)`, cutoff)
	if err != nil {
		return 0, err
	}
	type orphan struct{ id, storage, thumbnail string }
	var orphans []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.id, &o.storage, &o.thumbnail); err != nil {
			rows.Close()
			return 0, err
		}
		orphans = append(orphans, o)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(orphans) == 0 {
		return 0, nil
	}
	tx, err := a.store.DB().Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, o := range orphans {
		if _, err := tx.Exec(`DELETE FROM files WHERE id=? AND status='ready' AND NOT EXISTS (SELECT 1 FROM message_attachments WHERE file_id=?) AND NOT EXISTS (SELECT 1 FROM group_message_attachments WHERE file_id=?)`, o.id, o.id, o.id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	removed := 0
	for _, o := range orphans {
		// Database metadata is already removed, so a filesystem cleanup failure
		// is safe to report without making a referenced record inaccessible.
		if err := os.Remove(o.storage); err == nil || os.IsNotExist(err) {
			removed++
		}
		if o.thumbnail != "" {
			_ = os.Remove(o.thumbnail)
		}
	}
	return removed, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func fileModTime(path string) time.Time {
	if st, err := os.Stat(path); err == nil {
		return st.ModTime()
	}
	return time.Unix(0, 0)
}
func contentDisposition(kind, name string) string {
	return kind + `; filename="` + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(name, `"`, `'`), "\r", ""), "\n", "") + `"`
}

func quotaFor(a *API, owner string) int64 { return a.deviceStorageQuota() }

func (a *API) attachmentsForMessages(messageIDs []string) (map[string][]Attachment, error) {
	result := make(map[string][]Attachment, len(messageIDs))
	ids := uniqueNonEmptyIDs(messageIDs)
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := a.store.DB().Query(`SELECT ma.message_id,f.id,f.owner_id,f.original_name,f.content_type,f.size,f.sha256,COALESCE(f.width,0),COALESCE(f.height,0),f.is_image,f.storage_path,COALESCE(f.thumbnail_path,''),f.created_at,f.status FROM message_attachments ma JOIN files f ON f.id=ma.file_id WHERE ma.message_id IN (`+placeholders+`) ORDER BY ma.message_id,ma.position,ma.file_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var messageID string
		var r fileRecord
		var imageFlag int
		var width, height sql.NullInt64
		if err := rows.Scan(&messageID, &r.ID, &r.OwnerID, &r.OriginalName, &r.ContentType, &r.Size, &r.SHA256, &width, &height, &imageFlag, &r.StoragePath, &r.ThumbnailPath, &r.CreatedAt, &r.Status); err != nil {
			return nil, err
		}
		if width.Valid {
			r.Width = int(width.Int64)
		}
		if height.Valid {
			r.Height = int(height.Int64)
		}
		r.IsImage = imageFlag != 0
		r.DownloadURL = "/api/v1/files/" + r.ID + "/content"
		if r.ThumbnailPath != "" {
			r.ThumbnailURL = "/api/v1/files/" + r.ID + "/thumbnail"
		}
		if r.Status == "ready" {
			result[messageID] = append(result[messageID], r.Attachment)
		}
	}
	return result, rows.Err()
}

func (a *API) attachmentsForGroupMessages(messageIDs []string) (map[string][]Attachment, error) {
	result := make(map[string][]Attachment, len(messageIDs))
	ids := uniqueNonEmptyIDs(messageIDs)
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := a.store.DB().Query(`SELECT ma.group_message_id,f.id,f.owner_id,f.original_name,f.content_type,f.size,f.sha256,COALESCE(f.width,0),COALESCE(f.height,0),f.is_image,f.storage_path,COALESCE(f.thumbnail_path,''),f.created_at,f.status FROM group_message_attachments ma JOIN files f ON f.id=ma.file_id WHERE ma.group_message_id IN (`+placeholders+`) ORDER BY ma.group_message_id,ma.position,ma.file_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var messageID string
		var r fileRecord
		var imageFlag int
		var width, height sql.NullInt64
		if err := rows.Scan(&messageID, &r.ID, &r.OwnerID, &r.OriginalName, &r.ContentType, &r.Size, &r.SHA256, &width, &height, &imageFlag, &r.StoragePath, &r.ThumbnailPath, &r.CreatedAt, &r.Status); err != nil {
			return nil, err
		}
		if width.Valid {
			r.Width = int(width.Int64)
		}
		if height.Valid {
			r.Height = int(height.Int64)
		}
		r.IsImage = imageFlag != 0
		r.DownloadURL = "/api/v1/files/" + r.ID + "/content"
		if r.ThumbnailPath != "" {
			r.ThumbnailURL = "/api/v1/files/" + r.ID + "/thumbnail"
		}
		if r.Status == "ready" {
			result[messageID] = append(result[messageID], r.Attachment)
		}
	}
	return result, rows.Err()
}

func uniqueNonEmptyIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
