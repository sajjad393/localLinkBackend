package httpapi

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxDisplayNameLen = 80
	maxAvatarBytes    = int64(3 * 1024 * 1024)
	maxAvatarPixels   = int64(16_000_000)
)

type Profile struct {
	UserID               string `json:"user_id"`
	DeviceID             string `json:"device_id,omitempty"`
	Username             string `json:"username"`
	DisplayName          string `json:"display_name"`
	PhoneNumber          string `json:"phone_number,omitempty"`
	PhoneVisibility      string `json:"phone_visibility,omitempty"`
	DiscoverableByPhone  bool   `json:"discoverable_by_phone"`
	DiscoverableByName   bool   `json:"discoverable_by_name"`
	DirectorySyncEnabled bool   `json:"directory_sync_enabled"`
	ProfileVersion       int64  `json:"profile_version"`
	AvatarURL            string `json:"avatar_url,omitempty"`
	AvatarUpdatedAt      string `json:"avatar_updated_at,omitempty"`
	UpdatedAt            string `json:"updated_at"`
}

func (a *API) ensureProfile(userID, username, displayName, now string) error {
	if strings.TrimSpace(displayName) == "" {
		displayName = username
	}
	_, err := a.store.DB().Exec(`INSERT INTO profiles(user_id,display_name,avatar_path,avatar_content_type,avatar_size,avatar_sha256,avatar_updated_at,created_at,updated_at)
		VALUES(?,?,NULL,NULL,0,'',NULL,?,?) ON CONFLICT(user_id) DO UPDATE SET updated_at=excluded.updated_at`,
		userID, strings.TrimSpace(displayName), now, now)
	return err
}

func (a *API) currentAccountForDevice(r *http.Request) (deviceID, userID, username string, ok bool) {
	deviceID, ok = a.authorizeDevice(r)
	if !ok {
		return "", "", "", false
	}
	if err := a.store.DB().QueryRow(`SELECT u.id,u.username FROM devices d JOIN users u ON u.id=d.user_id WHERE d.id=? AND u.status='active'`, deviceID).Scan(&userID, &username); err != nil {
		return "", "", "", false
	}
	return deviceID, userID, username, true
}

func (a *API) GetProfile(w http.ResponseWriter, r *http.Request) {
	_, userID, username, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var displayName, avatarPath, avatarUpdatedAt, updatedAt, phoneNumber, phoneVisibility string
	var avatarSize int64
	var discoverableByPhone, discoverableByName, directorySyncEnabled, profileVersion int64
	err := a.store.DB().QueryRow(`SELECT display_name,COALESCE(avatar_path,''),COALESCE(avatar_updated_at,''),updated_at,COALESCE(avatar_size,0),COALESCE(phone_number,''),COALESCE(phone_visibility,'contacts'),COALESCE(discoverable_by_phone,1),COALESCE(discoverable_by_name,1),COALESCE(directory_sync_enabled,1),COALESCE(profile_version,0)
		FROM profiles WHERE user_id=?`, userID).Scan(&displayName, &avatarPath, &avatarUpdatedAt, &updatedAt, &avatarSize, &phoneNumber, &phoneVisibility, &discoverableByPhone, &discoverableByName, &directorySyncEnabled, &profileVersion)
	if err == sql.ErrNoRows {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := a.ensureProfile(userID, username, username, now); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create profile")
			return
		}
		displayName = username
		phoneNumber = ""
		phoneVisibility = "contacts"
		discoverableByPhone = 1
		discoverableByName = 1
		directorySyncEnabled = 1
		profileVersion = 0
		avatarUpdatedAt = ""
		updatedAt = now
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read profile")
		return
	}
	avatarURL := ""
	if avatarPath != "" && avatarSize > 0 {
		avatarURL = "/api/v1/profile/avatar"
	}
	writeJSON(w, http.StatusOK, Profile{
		UserID:               userID,
		Username:             username,
		DisplayName:          displayName,
		PhoneNumber:          phoneNumber,
		PhoneVisibility:      phoneVisibility,
		DiscoverableByPhone:  discoverableByPhone != 0,
		DiscoverableByName:   discoverableByName != 0,
		DirectorySyncEnabled: directorySyncEnabled != 0,
		ProfileVersion:       profileVersion,
		AvatarURL:            avatarURL,
		AvatarUpdatedAt:      avatarUpdatedAt,
		UpdatedAt:            updatedAt,
	})
}

func (a *API) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	_, userID, currentUsername, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in struct {
		DisplayName          string  `json:"display_name"`
		Username             string  `json:"username"`
		PhoneNumber          *string `json:"phone_number"`
		PhoneVisibility      string  `json:"phone_visibility"`
		DiscoverableByPhone  *bool   `json:"discoverable_by_phone"`
		DiscoverableByName   *bool   `json:"discoverable_by_name"`
		DirectorySyncEnabled *bool   `json:"directory_sync_enabled"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		displayName = currentUsername
	}
	if len(displayName) > maxDisplayNameLen || hasControlChars(displayName) {
		writeError(w, http.StatusBadRequest, "invalid display name")
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if username == "" {
		username = currentUsername
	}
	if !validUsername(username) {
		writeError(w, http.StatusBadRequest, "invalid username")
		return
	}
	phoneInput := ""
	if in.PhoneNumber != nil {
		phoneInput = *in.PhoneNumber
	}
	phoneNumber := normalizePhoneNumber(phoneInput)
	if in.PhoneNumber != nil && strings.TrimSpace(phoneInput) != "" && phoneNumber == "" {
		writeError(w, http.StatusBadRequest, "invalid phone number")
		return
	}
	phoneVisibility := strings.ToLower(strings.TrimSpace(in.PhoneVisibility))
	if phoneVisibility == "" {
		phoneVisibility = "contacts"
	}
	if phoneVisibility != "private" && phoneVisibility != "contacts" && phoneVisibility != "public" {
		writeError(w, http.StatusBadRequest, "invalid phone visibility")
		return
	}
	discoverableByPhone, discoverableByName, directorySyncEnabled := true, true, true
	var currentPhone, currentVisibility string
	var currentPhoneDiscovery, currentNameDiscovery, currentSync, currentVersion int64
	_ = a.store.DB().QueryRow(`SELECT COALESCE(phone_number,''),COALESCE(phone_visibility,'contacts'),COALESCE(discoverable_by_phone,1),COALESCE(discoverable_by_name,1),COALESCE(directory_sync_enabled,1),COALESCE(profile_version,0) FROM profiles WHERE user_id=?`, userID).Scan(&currentPhone, &currentVisibility, &currentPhoneDiscovery, &currentNameDiscovery, &currentSync, &currentVersion)
	if in.PhoneNumber == nil {
		phoneNumber = currentPhone
	}
	if strings.TrimSpace(in.PhoneVisibility) == "" {
		phoneVisibility = currentVisibility
	}
	if in.DiscoverableByPhone != nil {
		discoverableByPhone = *in.DiscoverableByPhone
	} else {
		discoverableByPhone = currentPhoneDiscovery != 0
	}
	if in.DiscoverableByName != nil {
		discoverableByName = *in.DiscoverableByName
	} else {
		discoverableByName = currentNameDiscovery != 0
	}
	if in.DirectorySyncEnabled != nil {
		directorySyncEnabled = *in.DirectorySyncEnabled
	} else {
		directorySyncEnabled = currentSync != 0
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start profile update")
		return
	}
	defer tx.Rollback()
	if username != currentUsername {
		if _, err = tx.Exec(`UPDATE users SET username=?,updated_at=? WHERE id=? AND status='active'`, username, now, userID); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				writeError(w, http.StatusConflict, "username is already registered")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to update username")
			}
			return
		}
	}
	if _, err = tx.Exec(`INSERT INTO profiles(user_id,display_name,avatar_path,avatar_content_type,avatar_size,avatar_sha256,avatar_updated_at,phone_number,phone_visibility,discoverable_by_phone,discoverable_by_name,directory_sync_enabled,profile_version,created_at,updated_at)
		VALUES(?,?,NULL,NULL,0,'',NULL,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET display_name=excluded.display_name,phone_number=excluded.phone_number,phone_visibility=excluded.phone_visibility,discoverable_by_phone=excluded.discoverable_by_phone,discoverable_by_name=excluded.discoverable_by_name,directory_sync_enabled=excluded.directory_sync_enabled,profile_version=profiles.profile_version+1,updated_at=excluded.updated_at`, userID, displayName, phoneNumber, phoneVisibility, boolInt(discoverableByPhone), boolInt(discoverableByName), boolInt(directorySyncEnabled), 1, now, now); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") && phoneNumber != "" {
			writeError(w, http.StatusConflict, "phone number is already registered")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update profile")
		}
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save profile")
		return
	}
	a.audit("profile_update", userID, "username="+username)
	a.GetProfile(w, r)
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func normalizePhoneNumber(input string) string {
	raw := strings.TrimSpace(input)
	raw = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "").Replace(raw)
	if strings.HasPrefix(raw, "00") {
		raw = "+" + raw[2:]
	}
	if strings.HasPrefix(raw, "03") && len(raw) == 11 {
		raw = "+92" + raw[1:]
	}
	if strings.HasPrefix(raw, "923") && len(raw) == 12 {
		raw = "+" + raw
	}
	if !strings.HasPrefix(raw, "+") {
		return ""
	}
	digits := raw[1:]
	if len(digits) < 8 || len(digits) > 15 {
		return ""
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return "+" + digits
}

func (a *API) UploadProfileAvatar(w http.ResponseWriter, r *http.Request) {
	_, userID, username, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if r.ContentLength > maxAvatarBytes+(1<<20) {
		writeError(w, http.StatusRequestEntityTooLarge, "profile image is too large")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
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

	data, err := readLimited(src, maxAvatarBytes)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "profile image is too large")
		return
	}
	contentType, width, height, err := validateImageData(data, header.Header.Get("Content-Type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "profile image must be a valid image")
		return
	}
	if int64(width)*int64(height) > maxAvatarPixels {
		writeError(w, http.StatusBadRequest, "profile image dimensions are too large")
		return
	}

	dir := filepath.Join(a.fileRoot, "profiles", userID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create profile storage")
		return
	}
	ext := extensionForContentType(contentType)
	tempPath := filepath.Join(dir, ".avatar-"+uuid.NewString()+".tmp")
	finalPath := filepath.Join(dir, "avatar"+ext)
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store profile image")
		return
	}
	defer os.Remove(tempPath)
	oldPath := ""
	var avatarUpdatedAt string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(avatar_path,''),COALESCE(avatar_updated_at,'') FROM profiles WHERE user_id=?`, userID).Scan(&oldPath, &avatarUpdatedAt); err != nil && err != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to read profile image state")
		return
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finalize profile image")
		return
	}
	sha := sha256.Sum256(data)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = a.store.DB().Exec(`INSERT INTO profiles(user_id,display_name,avatar_path,avatar_content_type,avatar_size,avatar_sha256,avatar_updated_at,created_at,updated_at)
		VALUES(?,?,?, ?,?,?, ?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET avatar_path=excluded.avatar_path,avatar_content_type=excluded.avatar_content_type,avatar_size=excluded.avatar_size,avatar_sha256=excluded.avatar_sha256,avatar_updated_at=excluded.avatar_updated_at,updated_at=excluded.updated_at`,
		userID, username, "profiles/"+userID+"/avatar"+ext, contentType, len(data), hex.EncodeToString(sha[:]), now, now, now)
	if err != nil {
		_ = os.Remove(finalPath)
		writeError(w, http.StatusInternalServerError, "failed to update profile image")
		return
	}
	if oldPath != "" && oldPath != "profiles/"+userID+"/avatar"+ext {
		_ = os.Remove(filepath.Join(a.fileRoot, filepath.FromSlash(oldPath)))
	}
	_ = avatarUpdatedAt
	a.audit("profile_avatar_update", userID, "size="+fmt.Sprintf("%d", len(data)))
	a.GetProfile(w, r)
}

func (a *API) GetProfileAvatar(w http.ResponseWriter, r *http.Request) {
	_, userID, _, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var path, contentType, checksum string
	var size int64
	if err := a.store.DB().QueryRow(`SELECT COALESCE(avatar_path,''),COALESCE(avatar_content_type,''),COALESCE(avatar_sha256,''),COALESCE(avatar_size,0) FROM profiles WHERE user_id=?`, userID).Scan(&path, &contentType, &checksum, &size); err != nil {
		writeError(w, http.StatusNotFound, "profile image not found")
		return
	}
	if path == "" || size <= 0 {
		writeError(w, http.StatusNotFound, "profile image not found")
		return
	}
	full := filepath.Join(a.fileRoot, filepath.FromSlash(path))
	if err := ensureInside(a.fileRoot, full); err != nil {
		writeError(w, http.StatusInternalServerError, "invalid profile image path")
		return
	}
	file, err := os.Open(full)
	if err != nil {
		writeError(w, http.StatusNotFound, "profile image not found")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if checksum != "" {
		w.Header().Set("ETag", `"`+checksum+`"`)
	}
	http.ServeContent(w, r, filepath.Base(full), time.Time{}, file)
}

func readLimited(src io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(src, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("too large")
	}
	return data, nil
}

func validateImageData(data []byte, declared string) (string, int, int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || format == "" {
		return "", 0, 0, fmt.Errorf("invalid image")
	}
	contentType := "image/" + format
	if normalized, _, err := mime.ParseMediaType(strings.TrimSpace(declared)); err == nil && strings.HasPrefix(normalized, "image/") {
		contentType = normalized
	}
	// Trust the decoded format for storage so an arbitrary declared MIME type
	// cannot cause the server to serve a non-image payload as an image.
	switch format {
	case "jpeg":
		contentType = "image/jpeg"
	case "png":
		contentType = "image/png"
	case "gif":
		contentType = "image/gif"
	}
	return contentType, cfg.Width, cfg.Height, nil
}

func extensionForContentType(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}

func ensureInside(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes root")
	}
	return nil
}
