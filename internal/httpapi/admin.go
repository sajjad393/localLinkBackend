package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AdminSummary struct {
	Devices           int   `json:"devices"`
	OnlineDevices     int   `json:"online_devices"`
	Messages          int   `json:"messages"`
	Groups            int   `json:"groups"`
	Files             int   `json:"files"`
	FileBytes         int64 `json:"file_bytes"`
	ActiveCalls       int   `json:"active_calls"`
	DBBytes           int64 `json:"db_bytes"`
	FileStorageBytes  int64 `json:"file_storage_bytes"`
	Users             int   `json:"users"`
	ActiveSessions    int   `json:"active_sessions"`
	PendingRecoveries int   `json:"pending_recoveries"`
	SecurityAlerts    int   `json:"security_alerts"`
}

type AdminDevice struct {
	Device
	UserID          string `json:"user_id,omitempty"`
	Username        string `json:"username,omitempty"`
	Online          bool   `json:"online"`
	TokenConfigured bool   `json:"token_configured"`
}

type AdminGroup struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnerID   string `json:"owner_id"`
	CreatedAt string `json:"created_at"`
	Members   int    `json:"members"`
}

type AdminLog struct {
	ID        int64  `json:"id"`
	AdminID   string `json:"admin_id,omitempty"`
	Action    string `json:"action"`
	TargetID  string `json:"target_id,omitempty"`
	Detail    string `json:"detail,omitempty"`
	CreatedAt string `json:"created_at"`
}

type AdminSecurityEvent struct {
	ID         int64  `json:"id"`
	Action     string `json:"action"`
	UserID     string `json:"user_id,omitempty"`
	DeviceID   string `json:"device_id,omitempty"`
	SourceHash string `json:"source_hash,omitempty"`
	Detail     string `json:"detail,omitempty"`
	CreatedAt  string `json:"created_at"`
}

func (a *API) adminConfigured() bool {
	var count int
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_accounts WHERE status='active'`).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func (a *API) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	_, ok := a.requireAdminPermission(w, r, "read")
	return ok
}

func (a *API) AdminSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	var s AdminSummary
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM devices").Scan(&s.Devices)
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM devices WHERE COALESCE(status,'active')='active' AND last_seen_at >= ?", time.Now().UTC().Add(-2*time.Minute).Format(time.RFC3339Nano)).Scan(&s.OnlineDevices)
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM messages").Scan(&s.Messages)
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM groups").Scan(&s.Groups)
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM files").Scan(&s.Files)
	_ = a.store.DB().QueryRow("SELECT COALESCE(SUM(size),0) FROM files").Scan(&s.FileBytes)
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM calls WHERE status IN ('ringing','connected')").Scan(&s.ActiveCalls)
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM users").Scan(&s.Users)
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM auth_sessions WHERE revoked_at IS NULL AND expires_at > ?", time.Now().UTC().Format(time.RFC3339Nano)).Scan(&s.ActiveSessions)
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM account_recovery_requests WHERE status='pending' AND expires_at > ?", time.Now().UTC().Format(time.RFC3339Nano)).Scan(&s.PendingRecoveries)
	_ = a.store.DB().QueryRow("SELECT COUNT(*) FROM security_audit WHERE created_at >= ?", time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339Nano)).Scan(&s.SecurityAlerts)
	if info, err := os.Stat(getenvOr(a.dbPath(), "")); err == nil {
		s.DBBytes = info.Size()
	}
	s.FileStorageBytes = directorySize(a.fileRoot)
	writeJSON(w, http.StatusOK, s)
}

func (a *API) AdminDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-devices")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	args := []any{}
	where := ""
	if params.Cursor != nil {
		where = `WHERE (name COLLATE NOCASE > ? OR (name COLLATE NOCASE=? AND id>?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT d.id,d.name,d.platform,d.created_at,d.last_seen_at,COALESCE(d.status,'active'),COALESCE(d.revoked_at,''),COALESCE(d.identity_public_key,''),CASE WHEN d.token_hash IS NULL OR d.token_hash='' THEN 0 ELSE 1 END,COALESCE(d.user_id,''),COALESCE(u.username,'') FROM devices d LEFT JOIN users u ON u.id=d.user_id `+wherePrefixDevice(where)+` ORDER BY d.name COLLATE NOCASE,d.id LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list admin devices")
		return
	}
	defer rows.Close()
	cutoff := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339Nano)
	out := make([]AdminDevice, 0, params.Limit)
	for rows.Next() {
		var d AdminDevice
		var tok int
		var status, revokedAt, publicKey string
		if err := rows.Scan(&d.ID, &d.Name, &d.Platform, &d.CreatedAt, &d.LastSeenAt, &status, &revokedAt, &publicKey, &tok, &d.UserID, &d.Username); err != nil {
			writeError(w, 500, "failed to read device")
			return
		}
		d.Status = status
		d.RevokedAt = revokedAt
		d.IdentityFingerprint = deviceIdentityFingerprint(publicKey)
		d.Online = status == "active" && d.LastSeenAt >= cutoff
		d.TokenConfigured = tok == 1
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read admin devices")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Devices []AdminDevice `json:"devices"`
		PageInfo
	}{Devices: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-devices", last.Name, last.ID, false)
	}
	writeJSON(w, 200, resp)
}

func (a *API) AdminGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-groups")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	args := []any{}
	where := ""
	if params.Cursor != nil {
		where = `WHERE (g.name COLLATE NOCASE > ? OR (g.name COLLATE NOCASE=? AND g.id>?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT g.id,g.name,g.owner_id,g.created_at,COUNT(gm.device_id) FROM groups g LEFT JOIN group_members gm ON gm.group_id=g.id `+where+` GROUP BY g.id ORDER BY g.name COLLATE NOCASE,g.id LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list groups")
		return
	}
	defer rows.Close()
	out := make([]AdminGroup, 0, params.Limit)
	for rows.Next() {
		var g AdminGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt, &g.Members); err != nil {
			writeError(w, 500, "failed to read group")
			return
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read groups")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Groups []AdminGroup `json:"groups"`
		PageInfo
	}{Groups: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-groups", last.Name, last.ID, false)
	}
	writeJSON(w, 200, resp)
}

func (a *API) AdminCalls(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-calls")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	args := []any{}
	where := ""
	if params.Cursor != nil {
		where = `WHERE (started_at < ? OR (started_at=? AND id<?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT id,caller_id,callee_id,status,started_at,COALESCE(answered_at,''),COALESCE(ended_at,''),duration_seconds,COALESCE(end_reason,''),COALESCE(last_activity_at,started_at) FROM calls `+where+` ORDER BY started_at DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list calls")
		return
	}
	defer rows.Close()
	out := make([]CallRecord, 0, params.Limit)
	for rows.Next() {
		var c CallRecord
		if err := rows.Scan(&c.ID, &c.CallerID, &c.CalleeID, &c.Status, &c.StartedAt, &c.AnsweredAt, &c.EndedAt, &c.DurationSeconds, &c.EndReason, &c.LastActivityAt); err != nil {
			writeError(w, 500, "failed to read call")
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read calls")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Calls []CallRecord `json:"calls"`
		PageInfo
	}{Calls: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-calls", last.StartedAt, last.ID, true)
	}
	writeJSON(w, 200, resp)
}

func (a *API) AdminSecurityEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-security-events")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	args := []any{}
	where := ""
	if params.Cursor != nil {
		where = `WHERE id<?`
		args = append(args, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT id,action,user_id,device_id,source_hash,detail,created_at FROM security_audit `+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list security events")
		return
	}
	defer rows.Close()
	out := make([]AdminSecurityEvent, 0, params.Limit)
	for rows.Next() {
		var x AdminSecurityEvent
		if err := rows.Scan(&x.ID, &x.Action, &x.UserID, &x.DeviceID, &x.SourceHash, &x.Detail, &x.CreatedAt); err != nil {
			writeError(w, 500, "failed to read security event")
			return
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read security events")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Events []AdminSecurityEvent `json:"security_events"`
		PageInfo
	}{Events: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-security-events", fmt.Sprintf("%d", last.ID), fmt.Sprintf("%d", last.ID), true)
	}
	writeJSON(w, 200, resp)
}

func (a *API) AdminLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-logs")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	args := []any{}
	where := ""
	if params.Cursor != nil {
		where = `WHERE id<?`
		args = append(args, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT id,admin_id,action,target_id,detail,created_at FROM admin_audit `+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list audit logs")
		return
	}
	defer rows.Close()
	out := make([]AdminLog, 0, params.Limit)
	for rows.Next() {
		var x AdminLog
		if err := rows.Scan(&x.ID, &x.AdminID, &x.Action, &x.TargetID, &x.Detail, &x.CreatedAt); err != nil {
			writeError(w, 500, "failed to read audit log")
			return
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read audit logs")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Logs []AdminLog `json:"logs"`
		PageInfo
	}{Logs: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-logs", fmt.Sprintf("%d", last.ID), fmt.Sprintf("%d", last.ID), true)
	}
	writeJSON(w, 200, resp)
}

func (a *API) AdminRenameDevice(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_devices")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if id == "" || name == "" || len(name) > maxDeviceName || hasControlChars(name) {
		writeError(w, 400, "invalid device name")
		return
	}
	res, err := a.store.DB().Exec("UPDATE devices SET name=? WHERE id=?", name, id)
	if err != nil {
		writeError(w, 500, "failed to rename device")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, 404, "device not found")
		return
	}
	a.adminAudit(admin.ID, "rename_device", id, "name="+name)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) AdminRevokeDevice(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_devices")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "device id required")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := a.store.DB().Exec(`UPDATE devices SET status='revoked',revoked_at=? WHERE id=? AND COALESCE(status,'active')!='revoked'`, now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke device")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists int
		if err := a.store.DB().QueryRow(`SELECT 1 FROM devices WHERE id=?`, id).Scan(&exists); err != nil {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "already_revoked": true})
		return
	}
	_, _ = a.store.DB().Exec(`UPDATE auth_sessions SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, now, id)
	_, _ = a.store.DB().Exec(`UPDATE device_identity_keys SET retired_at=? WHERE device_id=? AND retired_at IS NULL`, now, id)
	a.hub.CloseDevice(id)
	a.presenceMu.Lock()
	delete(a.presence, id)
	a.presenceMu.Unlock()
	a.adminAudit(admin.ID, "revoke_device", id, "soft-revoked")
	a.auditSecurity("device_revoked", "", id, registrationKey(r), "admin revoke")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": true})
}

func (a *API) AdminBackup(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "backup")
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	dir := strings.TrimSpace(os.Getenv("LOCALLINK_BACKUP_DIR"))
	if dir == "" {
		dir = "data/backups"
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		writeError(w, 500, "failed to create backup directory")
		return
	}
	name := "locallink-" + time.Now().UTC().Format("20060102-150405") + ".db"
	dest := filepath.Join(dir, name)
	if _, err := a.store.DB().Exec("VACUUM INTO ?", dest); err != nil {
		writeError(w, 500, "backup failed")
		return
	}
	a.adminAudit(admin.ID, "backup_database", "", name)
	writeJSON(w, 200, map[string]any{"ok": true, "file": name, "path": dest})
}

func (a *API) audit(action, target, detail string) {
	a.adminAudit("", action, target, detail)
}
func (a *API) dbPath() string {
	v := strings.TrimSpace(os.Getenv("LOCALLINK_DB"))
	if v == "" {
		return "data/locallink.db"
	}
	return v
}
func getenvOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
func directorySize(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func wherePrefixDevice(where string) string {
	if where == "" {
		return ""
	}
	return strings.ReplaceAll(strings.ReplaceAll(where, "name COLLATE NOCASE", "d.name COLLATE NOCASE"), "id>", "d.id>")
}
