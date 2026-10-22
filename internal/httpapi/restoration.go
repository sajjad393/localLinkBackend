package httpapi

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRestorePage = 500
const maxCryptoBackupBytes = 1 << 20

type AccountRestoreManifest struct {
	Version                int              `json:"version"`
	Account                Account          `json:"account"`
	Profile                Profile          `json:"profile"`
	Devices                []Device         `json:"devices"`
	Counts                 map[string]int64 `json:"counts"`
	CryptoBackupAvailable  bool             `json:"crypto_backup_available"`
	CryptoBackupUpdatedAt  string           `json:"crypto_backup_updated_at,omitempty"`
	HistoricalIdentityKeys int64            `json:"historical_identity_keys"`
	GeneratedAt            string           `json:"generated_at"`
}

type AccountRestoreData struct {
	Version       int            `json:"version"`
	Scope         string         `json:"scope"`
	Account       *Account       `json:"account,omitempty"`
	Profile       *Profile       `json:"profile,omitempty"`
	Devices       []Device       `json:"devices,omitempty"`
	Groups        []Group        `json:"groups,omitempty"`
	GroupMembers  []GroupMember  `json:"group_members,omitempty"`
	Messages      []Message      `json:"messages,omitempty"`
	GroupMessages []GroupMessage `json:"group_messages,omitempty"`
	Calls         []CallRecord   `json:"calls,omitempty"`
	Files         []Attachment   `json:"files,omitempty"`
	Contacts      []AccountContact `json:"contacts,omitempty"`
	NextCursor    string         `json:"next_cursor,omitempty"`
	HasMore       bool           `json:"has_more"`
}

type restoreCursor struct {
	Time string `json:"time"`
	ID   string `json:"id"`
}

func encodeRestoreCursor(t, id string) string {
	b, _ := json.Marshal(restoreCursor{Time: t, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeRestoreCursor(raw string) (restoreCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return restoreCursor{}, nil
	}
	if len(raw) > 512 {
		return restoreCursor{}, errors.New("cursor too long")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return restoreCursor{}, errors.New("invalid cursor")
	}
	var c restoreCursor
	if err := json.Unmarshal(b, &c); err != nil || c.ID == "" || c.Time == "" || len(c.ID) > 128 || len(c.Time) > 128 {
		return restoreCursor{}, errors.New("invalid cursor")
	}
	return c, nil
}

func restoreLimit(r *http.Request) (int, error) {
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxRestorePage {
			return 0, errors.New("limit must be between 1 and 500")
		}
		limit = n
	}
	return limit, nil
}

func (a *API) accountUserForRequest(r *http.Request) (string, string, bool) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		return "", "", false
	}
	var userID, username string
	if err := a.store.DB().QueryRow(`SELECT u.id,u.username FROM devices d JOIN users u ON u.id=d.user_id WHERE d.id=? AND u.status='active' AND COALESCE(d.status,'active')='active'`, deviceID).Scan(&userID, &username); err != nil {
		return "", "", false
	}
	return deviceID, userID, true
}

func (a *API) GetAccountRestoreManifest(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := a.accountUserForRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var account Account
	if err := a.store.DB().QueryRow(`SELECT id,username,created_at FROM users WHERE id=? AND status='active'`, userID).Scan(&account.ID, &account.Username, &account.CreatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read account")
		return
	}
	profile := profileForUserDB(a.store.DB(), userID, account.Username)
	if profile.UserID == "" {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := a.ensureProfile(userID, account.Username, account.Username, now); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create profile")
			return
		}
		profile = profileForUserDB(a.store.DB(), userID, account.Username)
	}

	devices, err := a.accountDevicesForRestore(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read account devices")
		return
	}
	counts := map[string]int64{}
	queries := map[string]string{
		"messages":       `SELECT COUNT(*) FROM messages m WHERE EXISTS (SELECT 1 FROM devices d WHERE d.user_id=? AND (d.id=m.sender_id OR d.id=m.recipient_id))`,
		"group_messages": `SELECT COUNT(*) FROM group_messages gm WHERE EXISTS (SELECT 1 FROM group_members gmem JOIN devices d ON d.id=gmem.device_id WHERE gmem.group_id=gm.group_id AND d.user_id=?)`,
		"calls":          `SELECT COUNT(*) FROM calls c WHERE EXISTS (SELECT 1 FROM devices d WHERE d.user_id=? AND (d.id=c.caller_id OR d.id=c.callee_id))`,
		"contacts":       `SELECT COUNT(*) FROM account_contacts WHERE user_id=?`,
		"files":          `SELECT COUNT(DISTINCT f.id) FROM files f WHERE f.status='ready' AND (EXISTS (SELECT 1 FROM devices d WHERE d.user_id=? AND d.id=f.owner_id) OR EXISTS (SELECT 1 FROM message_attachments ma JOIN messages m ON m.id=ma.message_id JOIN devices d ON d.user_id=? AND (d.id=m.sender_id OR d.id=m.recipient_id) WHERE ma.file_id=f.id) OR EXISTS (SELECT 1 FROM group_message_attachments ga JOIN group_messages gm ON gm.id=ga.group_message_id JOIN group_members gmem ON gmem.group_id=gm.group_id JOIN devices d ON d.id=gmem.device_id AND d.user_id=? WHERE ga.file_id=f.id))`,
	}
	for key, query := range queries {
		var n int64
		args := []any{userID}
		if key == "files" {
			args = []any{userID, userID, userID}
		}
		if err := a.store.DB().QueryRow(query, args...).Scan(&n); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to count restored "+key)
			return
		}
		counts[key] = n
	}
	var historical int64
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM device_identity_keys dik JOIN devices d ON d.id=dik.device_id WHERE d.user_id=?`, userID).Scan(&historical); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count identity keys")
		return
	}
	var backupAt string
	err = a.store.DB().QueryRow(`SELECT COALESCE(updated_at,'') FROM account_crypto_backups WHERE user_id=?`, userID).Scan(&backupAt)
	available := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to inspect crypto backup")
		return
	}
	writeJSON(w, http.StatusOK, AccountRestoreManifest{
		Version: 1, Account: account, Profile: profile, Devices: devices, Counts: counts,
		CryptoBackupAvailable: available, CryptoBackupUpdatedAt: backupAt,
		HistoricalIdentityKeys: historical, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (a *API) GetAccountRestoreData(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := a.accountUserForRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	limit, err := restoreLimit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cursor, err := decodeRestoreCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := AccountRestoreData{Version: 1, Scope: scope, Messages: []Message{}, GroupMessages: []GroupMessage{}, Calls: []CallRecord{}, Files: []Attachment{}, Groups: []Group{}, GroupMembers: []GroupMember{}, Devices: []Device{}, Contacts: []AccountContact{}}
	switch scope {
	case "metadata":
		if err := a.fillRestoreMetadata(&resp, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to build account restore metadata")
			return
		}
	case "messages":
		if err := a.fillRestoreMessages(&resp, userID, cursor, limit); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to build message restore data")
			return
		}
	case "group_messages":
		if err := a.fillRestoreGroupMessages(&resp, userID, cursor, limit); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to build group message restore data")
			return
		}
	case "calls":
		if err := a.fillRestoreCalls(&resp, userID, cursor, limit); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to build call restore data")
			return
		}
	case "files":
		if err := a.fillRestoreFiles(&resp, userID, cursor, limit); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to build file restore data")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "scope must be metadata, messages, group_messages, calls, or files")
		return
	}
	a.audit("account_restore_export", userID, "scope="+scope)
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) fillRestoreMetadata(resp *AccountRestoreData, userID string) error {
	var account Account
	if err := a.store.DB().QueryRow(`SELECT id,username,created_at FROM users WHERE id=?`, userID).Scan(&account.ID, &account.Username, &account.CreatedAt); err != nil {
		return err
	}
	profile := profileForUserDB(a.store.DB(), userID, account.Username)
	devices, err := a.accountDevicesForRestore(userID)
	if err != nil {
		return err
	}
	groups, members, err := a.accountGroupsForRestore(userID)
	if err != nil {
		return err
	}
	contacts, err := a.accountContactsForUser(userID)
	if err != nil {
		return err
	}
	resp.Account = &account
	resp.Profile = &profile
	resp.Devices = devices
	resp.Groups = groups
	resp.GroupMembers = members
	resp.Contacts = contacts
	return nil
}

func (a *API) accountDevicesForRestore(userID string) ([]Device, error) {
	rows, err := a.store.DB().Query(`SELECT id,name,platform,created_at,last_seen_at,COALESCE(status,'active'),COALESCE(revoked_at,''),COALESCE(identity_public_key,'') FROM devices WHERE user_id=? ORDER BY created_at ASC,id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Device, 0)
	for rows.Next() {
		var d Device
		var status, revokedAt, publicKey string
		if err := rows.Scan(&d.ID, &d.Name, &d.Platform, &d.CreatedAt, &d.LastSeenAt, &status, &revokedAt, &publicKey); err != nil {
			return nil, err
		}
		d.Status = status
		d.RevokedAt = revokedAt
		d.IdentityFingerprint = deviceIdentityFingerprint(publicKey)
		d = a.enrichDevicePresence(d)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (a *API) accountGroupsForRestore(userID string) ([]Group, []GroupMember, error) {
	rows, err := a.store.DB().Query(`SELECT DISTINCT g.id,g.name,g.owner_id,g.created_at FROM groups g JOIN group_members gm ON gm.group_id=g.id JOIN devices d ON d.id=gm.device_id WHERE d.user_id=? ORDER BY g.created_at ASC,g.id ASC`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	groups := make([]Group, 0)
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt); err != nil {
			return nil, nil, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	members := make([]GroupMember, 0)
	for _, g := range groups {
		m, err := a.groupMembersForSync(g.ID)
		if err != nil {
			return nil, nil, err
		}
		members = append(members, m...)
	}
	return groups, members, nil
}

func (a *API) fillRestoreMessages(resp *AccountRestoreData, userID string, cursor restoreCursor, limit int) error {
	args := []any{userID}
	where := `EXISTS (SELECT 1 FROM devices d WHERE d.user_id=? AND (d.id=m.sender_id OR d.id=m.recipient_id))`
	if cursor.ID != "" {
		where += ` AND (m.created_at > ? OR (m.created_at=? AND m.id>?))`
		args = append(args, cursor.Time, cursor.Time, cursor.ID)
	}
	args = append(args, limit+1)
	rows, err := a.store.DB().Query(`SELECT m.id,m.sender_id,m.recipient_id,m.body,m.created_at,m.status,COALESCE(m.delivered_at,''),COALESCE(m.server_seq,0) FROM messages m WHERE `+where+` ORDER BY m.created_at ASC,m.id ASC LIMIT ?`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	rowsData := make([]Message, 0, limit+1)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &m.CreatedAt, &m.Status, &m.DeliveredAt, &m.ServerSeq); err != nil {
			return err
		}
		rowsData = append(rowsData, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	messageIDs := make([]string, 0, len(rowsData))
	for _, m := range rowsData {
		messageIDs = append(messageIDs, m.ID)
	}
	attachmentMap, err := a.attachmentsForMessages(messageIDs)
	if err != nil {
		return err
	}
	for _, m := range rowsData {
		m.Attachments = attachmentMap[m.ID]
		resp.Messages = append(resp.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(resp.Messages) > limit {
		resp.HasMore = true
		resp.Messages = resp.Messages[:limit]
	}
	if len(resp.Messages) > 0 {
		last := resp.Messages[len(resp.Messages)-1]
		resp.NextCursor = encodeRestoreCursor(last.CreatedAt, last.ID)
	}
	return nil
}

func (a *API) fillRestoreGroupMessages(resp *AccountRestoreData, userID string, cursor restoreCursor, limit int) error {
	args := []any{userID}
	where := `EXISTS (SELECT 1 FROM group_members gmem JOIN devices d ON d.id=gmem.device_id WHERE gmem.group_id=gm.group_id AND d.user_id=?)`
	if cursor.ID != "" {
		where += ` AND (gm.created_at > ? OR (gm.created_at=? AND gm.id>?))`
		args = append(args, cursor.Time, cursor.Time, cursor.ID)
	}
	args = append(args, limit+1)
	rows, err := a.store.DB().Query(`SELECT gm.id,gm.group_id,gm.sender_id,gm.body,gm.created_at,gm.server_seq FROM group_messages gm WHERE `+where+` ORDER BY gm.created_at ASC,gm.id ASC LIMIT ?`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	rowsData := make([]GroupMessage, 0, limit+1)
	for rows.Next() {
		var m GroupMessage
		if err := rows.Scan(&m.ID, &m.GroupID, &m.SenderID, &m.Body, &m.CreatedAt, &m.ServerSeq); err != nil {
			return err
		}
		rowsData = append(rowsData, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	messageIDs := make([]string, 0, len(rowsData))
	for _, m := range rowsData {
		messageIDs = append(messageIDs, m.ID)
	}
	attachmentMap, err := a.attachmentsForGroupMessages(messageIDs)
	if err != nil {
		return err
	}
	for _, m := range rowsData {
		m.Attachments = attachmentMap[m.ID]
		resp.GroupMessages = append(resp.GroupMessages, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(resp.GroupMessages) > limit {
		resp.HasMore = true
		resp.GroupMessages = resp.GroupMessages[:limit]
	}
	if len(resp.GroupMessages) > 0 {
		last := resp.GroupMessages[len(resp.GroupMessages)-1]
		resp.NextCursor = encodeRestoreCursor(last.CreatedAt, last.ID)
	}
	return nil
}

func (a *API) fillRestoreCalls(resp *AccountRestoreData, userID string, cursor restoreCursor, limit int) error {
	args := []any{userID}
	where := `EXISTS (SELECT 1 FROM devices d WHERE d.user_id=? AND (d.id=c.caller_id OR d.id=c.callee_id))`
	if cursor.ID != "" {
		where += ` AND (c.started_at > ? OR (c.started_at=? AND c.id>?))`
		args = append(args, cursor.Time, cursor.Time, cursor.ID)
	}
	args = append(args, limit+1)
	rows, err := a.store.DB().Query(`SELECT c.id,c.caller_id,c.callee_id,c.status,c.started_at,COALESCE(c.answered_at,''),COALESCE(c.ended_at,''),c.duration_seconds,COALESCE(c.end_reason,''),COALESCE(c.last_activity_at,c.started_at) FROM calls c WHERE `+where+` ORDER BY c.started_at ASC,c.id ASC LIMIT ?`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c CallRecord
		if err := rows.Scan(&c.ID, &c.CallerID, &c.CalleeID, &c.Status, &c.StartedAt, &c.AnsweredAt, &c.EndedAt, &c.DurationSeconds, &c.EndReason, &c.LastActivityAt); err != nil {
			return err
		}
		resp.Calls = append(resp.Calls, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(resp.Calls) > limit {
		resp.HasMore = true
		resp.Calls = resp.Calls[:limit]
	}
	if len(resp.Calls) > 0 {
		last := resp.Calls[len(resp.Calls)-1]
		resp.NextCursor = encodeRestoreCursor(last.StartedAt, last.ID)
	}
	return nil
}

func (a *API) fillRestoreFiles(resp *AccountRestoreData, userID string, cursor restoreCursor, limit int) error {
	args := []any{userID, userID, userID}
	where := `(EXISTS (SELECT 1 FROM devices d WHERE d.user_id=? AND d.id=f.owner_id) OR EXISTS (SELECT 1 FROM message_attachments ma JOIN messages m ON m.id=ma.message_id JOIN devices d ON d.user_id=? AND (d.id=m.sender_id OR d.id=m.recipient_id) WHERE ma.file_id=f.id) OR EXISTS (SELECT 1 FROM group_message_attachments ga JOIN group_messages gm ON gm.id=ga.group_message_id JOIN group_members gmem ON gmem.group_id=gm.group_id JOIN devices d ON d.id=gmem.device_id AND d.user_id=? WHERE ga.file_id=f.id))`
	if cursor.ID != "" {
		where += ` AND (f.created_at > ? OR (f.created_at=? AND f.id>?))`
		args = append(args, cursor.Time, cursor.Time, cursor.ID)
	}
	args = append(args, limit+1)
	rows, err := a.store.DB().Query(`SELECT DISTINCT f.id,f.owner_id,f.original_name,f.content_type,f.size,f.sha256,COALESCE(f.width,0),COALESCE(f.height,0),f.is_image,f.created_at,f.status,COALESCE(f.thumbnail_path,'') FROM files f WHERE f.status='ready' AND `+where+` ORDER BY f.created_at ASC,f.id ASC LIMIT ?`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var lastCreated, lastID string
	for rows.Next() {
		var id, owner, name, content, sha, created, status, thumb string
		var size int64
		var width, height int
		var image int
		if err := rows.Scan(&id, &owner, &name, &content, &size, &sha, &width, &height, &image, &created, &status, &thumb); err != nil {
			return err
		}
		download := "/api/v1/files/" + id + "/content"
		thumbURL := ""
		if thumb != "" {
			thumbURL = "/api/v1/files/" + id + "/thumbnail"
		}
		resp.Files = append(resp.Files, Attachment{ID: id, OriginalName: name, ContentType: content, Size: size, SHA256: sha, Width: width, Height: height, IsImage: image != 0, DownloadURL: download, ThumbnailURL: thumbURL})
		lastCreated, lastID = created, id
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(resp.Files) > limit {
		resp.HasMore = true
		resp.Files = resp.Files[:limit]
	}
	if lastID != "" {
		resp.NextCursor = encodeRestoreCursor(lastCreated, lastID)
	}
	return nil
}

func (a *API) GetCryptoBackup(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := a.accountUserForRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var envelope, createdAt, updatedAt string
	var size int64
	err := a.store.DB().QueryRow(`SELECT envelope,created_at,updated_at,size FROM account_crypto_backups WHERE user_id=?`, userID).Scan(&envelope, &createdAt, &updatedAt, &size)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read crypto backup")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "envelope": envelope, "size": size, "created_at": createdAt, "updated_at": updatedAt})
}

func (a *API) PutCryptoBackup(w http.ResponseWriter, r *http.Request) {
	deviceID, userID, ok := a.accountUserForRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in struct {
		Envelope string `json:"envelope"`
	}
	if err := decodeJSON(r, &in, maxCryptoBackupBytes+4096); err != nil {
		writeError(w, http.StatusBadRequest, "invalid crypto backup")
		return
	}
	in.Envelope = strings.TrimSpace(in.Envelope)
	if len(in.Envelope) == 0 || len(in.Envelope) > maxCryptoBackupBytes || !strings.HasPrefix(in.Envelope, "locallink-crypto-backup:v1:") {
		writeError(w, http.StatusBadRequest, "invalid crypto backup envelope")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := a.store.DB().Exec(`INSERT INTO account_crypto_backups(user_id,envelope,size,created_at,updated_at,source_device_id) VALUES(?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET envelope=excluded.envelope,size=excluded.size,updated_at=excluded.updated_at,source_device_id=excluded.source_device_id`, userID, in.Envelope, len(in.Envelope), now, now, deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store crypto backup")
		return
	}
	a.audit("crypto_backup_put", userID, "size="+strconv.Itoa(len(in.Envelope)))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updated_at": now, "size": len(in.Envelope)})
}

func (a *API) DeleteCryptoBackup(w http.ResponseWriter, r *http.Request) {
	deviceID, userID, ok := a.accountUserForRequest(r)
	_ = deviceID
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if _, err := a.store.DB().Exec(`DELETE FROM account_crypto_backups WHERE user_id=?`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete crypto backup")
		return
	}
	a.audit("crypto_backup_delete", userID, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
