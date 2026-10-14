package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

type AdminUser struct {
	ID              string `json:"id"`
	Username        string `json:"username"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	LastFailedLogin string `json:"last_failed_login_at,omitempty"`
	LockedUntil     string `json:"locked_until,omitempty"`
	Devices         int    `json:"devices"`
	OnlineDevices   int    `json:"online_devices"`
}

type AdminAuthSession struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	ExpiresAt  string `json:"expires_at"`
	RevokedAt  string `json:"revoked_at,omitempty"`
	Active     bool   `json:"active"`
}

type AdminOwnSession struct {
	ID         string `json:"id"`
	AdminID    string `json:"admin_id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	ExpiresAt  string `json:"expires_at"`
	RevokedAt  string `json:"revoked_at,omitempty"`
	Active     bool   `json:"active"`
	Current    bool   `json:"current"`
}

type AdminTransfer struct {
	ID          string `json:"id"`
	OwnerID     string `json:"owner_id"`
	Size        int64  `json:"size"`
	StorageSize int64  `json:"storage_size"`
	ContentType string `json:"content_type"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

type AdminNetworkHealth struct {
	Devices             int `json:"devices"`
	OnlineDevices       int `json:"online_devices"`
	ServerConnected     int `json:"server_connected"`
	LanAvailable        int `json:"lan_available"`
	WiFiDirectConnected int `json:"wifi_direct_connected"`
	MeshAvailable       int `json:"mesh_available"`
	ActiveCalls         int `json:"active_calls"`
	PendingTransfers    int `json:"pending_transfers"`
	PendingRecoveries   int `json:"pending_recoveries"`
}

type AdminSettings struct {
	DashboardRefreshSeconds int  `json:"dashboard_refresh_seconds"`
	SecurityAlertThreshold  int  `json:"security_alert_threshold"`
	ReadOnlyServerConfig    bool `json:"read_only_server_config"`
	BackupAvailable         bool `json:"backup_available"`
}

// AdminPrivacyPolicy reports what this server does with user content. Message
// bodies are stored as users write them and the owner role can read them
// through the conversation review endpoints, so the policy says so plainly
// instead of promising users something the server does not enforce.
type AdminPrivacyPolicy struct {
	PasswordsExposed             bool `json:"passwords_exposed"`
	PrivateKeysExposed           bool `json:"private_keys_exposed"`
	PrivateMessageBodiesExposed  bool `json:"private_message_bodies_exposed"`
	PrivateCallAudioExposed      bool `json:"private_call_audio_exposed"`
	DecryptedCallMediaExposed    bool `json:"decrypted_call_media_exposed"`
	PrivateFileContentsExposed   bool `json:"private_file_contents_exposed"`
	AdministrativeActionsAudited bool `json:"administrative_actions_audited"`
	ServerSideRoleChecks         bool `json:"server_side_role_checks"`
	// MessageBodyAccessRole names the role that may read message bodies, and
	// MessageBodyAccessAudited records that every such read is written to the
	// audit log. Both are reported here so the console never has to guess.
	MessageBodyAccessRole    string `json:"message_body_access_role"`
	MessageBodyAccessAudited bool   `json:"message_body_access_audited"`
}

func (a *API) AdminUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-users")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	args := []any{}
	clauses := []string{}
	if search != "" {
		search = strings.ToLower(search)
		clauses = append(clauses, "LOWER(u.username) LIKE ?")
		args = append(args, "%"+search+"%")
	}
	if params.Cursor != nil {
		clauses = append(clauses, "(u.username COLLATE NOCASE > ? OR (u.username COLLATE NOCASE=? AND u.id>?))")
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`
        SELECT u.id,u.username,COALESCE(u.status,'active'),u.created_at,u.updated_at,
               COALESCE(u.last_failed_login_at,''),COALESCE(u.locked_until,''),
               COUNT(DISTINCT d.id),
               SUM(CASE WHEN COALESCE(d.status,'active')='active' AND d.last_seen_at >= ? THEN 1 ELSE 0 END)
        FROM users u
        LEFT JOIN devices d ON d.user_id=u.id`+where+`
        GROUP BY u.id ORDER BY u.username COLLATE NOCASE,u.id LIMIT ?`, append([]any{time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339Nano)}, args...)...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	defer rows.Close()
	out := make([]AdminUser, 0, params.Limit)
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Status, &u.CreatedAt, &u.UpdatedAt, &u.LastFailedLogin, &u.LockedUntil, &u.Devices, &u.OnlineDevices); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read user")
			return
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read users")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Users []AdminUser `json:"users"`
		PageInfo
	}{Users: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-users", last.Username, last.ID, false)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) AdminSetUserStatus(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_users")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if id == "" || !map[string]bool{"active": true, "suspended": true, "disabled": true}[status] {
		writeError(w, http.StatusBadRequest, "invalid user id or status")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := a.store.DB().Exec(`UPDATE users SET status=?,updated_at=? WHERE id=?`, status, now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user status")
		return
	}
	if n := resAffected(res); n == 0 {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if status != "active" {
		_, _ = a.store.DB().Exec(`UPDATE auth_sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, now, id)
	}
	a.adminAudit(admin.ID, "set_user_status", id, "status="+status)
	a.auditSecurity("admin_user_status_changed", id, "", registrationKey(r), "status="+status)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

func (a *API) AdminSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-sessions")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{params.Limit + 1}
	where := ""
	if params.Cursor != nil {
		where = " WHERE s.created_at < ? OR (s.created_at=? AND s.id<?)"
		args = []any{params.Cursor.Key, params.Cursor.Key, params.Cursor.ID, params.Limit + 1}
	}
	rows, err := a.store.DB().Query(`SELECT s.id,s.user_id,u.username,s.device_id,d.name,d.platform,s.created_at,s.last_seen_at,s.expires_at,COALESCE(s.revoked_at,'') FROM auth_sessions s JOIN users u ON u.id=s.user_id JOIN devices d ON d.id=s.device_id`+where+` ORDER BY s.created_at DESC,s.id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	defer rows.Close()
	now := time.Now().UTC()
	out := make([]AdminAuthSession, 0, params.Limit)
	for rows.Next() {
		var x AdminAuthSession
		if err := rows.Scan(&x.ID, &x.UserID, &x.Username, &x.DeviceID, &x.DeviceName, &x.Platform, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt, &x.RevokedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read session")
			return
		}
		exp, _ := time.Parse(time.RFC3339Nano, x.ExpiresAt)
		x.Active = x.RevokedAt == "" && exp.After(now)
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read sessions")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Sessions []AdminAuthSession `json:"sessions"`
		PageInfo
	}{Sessions: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-sessions", last.CreatedAt, last.ID, true)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) AdminRevokeSession(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_sessions")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := a.store.DB().Exec(`UPDATE auth_sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke session")
		return
	}
	if resAffected(res) == 0 {
		writeError(w, http.StatusNotFound, "session not found or already revoked")
		return
	}
	a.adminAudit(admin.ID, "revoke_session", id, "user session revoked")
	a.auditSecurity("session_revoked", "", "", registrationKey(r), "admin revoked user session")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": true})
}

func (a *API) AdminOwnSessions(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "read")
	if !ok {
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	tokenHashValue := tokenHash(token)
	params, err := parsePageParams(r, "admin-own-sessions")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{admin.ID, params.Limit + 1}
	where := "WHERE s.admin_id=?"
	if params.Cursor != nil {
		where += " AND (s.created_at < ? OR (s.created_at=? AND s.id<?))"
		args = []any{admin.ID, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID, params.Limit + 1}
	}
	rows, err := a.store.DB().Query(`SELECT s.id,s.admin_id,a.username,a.role,s.created_at,s.last_seen_at,s.expires_at,COALESCE(s.revoked_at,''),s.token_hash FROM admin_sessions s JOIN admin_accounts a ON a.id=s.admin_id `+where+` ORDER BY s.created_at DESC,s.id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list admin sessions")
		return
	}
	defer rows.Close()
	now := time.Now().UTC()
	out := make([]AdminOwnSession, 0, params.Limit)
	for rows.Next() {
		var x AdminOwnSession
		var storedHash string
		if err := rows.Scan(&x.ID, &x.AdminID, &x.Username, &x.Role, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt, &x.RevokedAt, &storedHash); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read admin session")
			return
		}
		exp, _ := time.Parse(time.RFC3339Nano, x.ExpiresAt)
		x.Active = x.RevokedAt == "" && exp.After(now)
		x.Current = secureTokenEqual(storedHash, tokenHashValue)
		out = append(out, x)
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Sessions []AdminOwnSession `json:"sessions"`
		PageInfo
	}{Sessions: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-own-sessions", last.CreatedAt, last.ID, true)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) AdminRevokeAdminSession(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_sessions")
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "admin session id required")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := a.store.DB().Exec(`UPDATE admin_sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke admin session")
		return
	}
	if resAffected(res) == 0 {
		writeError(w, http.StatusNotFound, "admin session not found or already revoked")
		return
	}
	a.adminAudit(admin.ID, "revoke_admin_session", id, "admin session revoked")
	a.auditSecurity("admin_session_revoked", "", "", registrationKey(r), "admin revoked admin session")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": true})
}

func (a *API) AdminTransfers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-transfers")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{}
	where := ""
	if params.Cursor != nil {
		where = " WHERE f.created_at < ? OR (f.created_at=? AND f.id<?)"
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT f.id,f.owner_id,f.size,COALESCE(f.storage_size,0),f.content_type,COALESCE(f.status,'ready'),f.created_at FROM files f`+where+` ORDER BY f.created_at DESC,f.id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list file transfers")
		return
	}
	defer rows.Close()
	out := make([]AdminTransfer, 0, params.Limit)
	for rows.Next() {
		var x AdminTransfer
		if err := rows.Scan(&x.ID, &x.OwnerID, &x.Size, &x.StorageSize, &x.ContentType, &x.Status, &x.CreatedAt); err != nil {
			writeError(w, 500, "failed to read file transfer")
			return
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read file transfers")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Transfers []AdminTransfer `json:"transfers"`
		PageInfo
	}{Transfers: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-transfers", last.CreatedAt, last.ID, true)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) AdminNetwork(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	now := time.Now().UTC()
	cutoff := now.Add(-presenceTTL)
	var n AdminNetworkHealth
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&n.Devices)
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM devices WHERE COALESCE(status,'active')='active' AND last_seen_at >= ?`, now.Add(-2*time.Minute).Format(time.RFC3339Nano)).Scan(&n.OnlineDevices)
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM calls WHERE status IN ('ringing','connected')`).Scan(&n.ActiveCalls)
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM files WHERE status IN ('uploading','processing','pending','in_progress')`).Scan(&n.PendingTransfers)
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM account_recovery_requests WHERE status='pending' AND expires_at > ?`, now.Format(time.RFC3339Nano)).Scan(&n.PendingRecoveries)
	a.presenceMu.RLock()
	for deviceID, p := range a.presence {
		if p.UpdatedAt == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, p.UpdatedAt)
		if err != nil || t.Before(cutoff) {
			continue
		}
		_ = deviceID
		if p.ServerConnected {
			n.ServerConnected++
		}
		if p.LanAvailable {
			n.LanAvailable++
		}
		if p.WiFiDirectConnected {
			n.WiFiDirectConnected++
		}
		if p.MeshAvailable {
			n.MeshAvailable++
		}
	}
	a.presenceMu.RUnlock()
	writeJSON(w, http.StatusOK, n)
}

func (a *API) AdminGetSettings(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "read")
	if !ok {
		return
	}
	_ = admin
	settings := AdminSettings{DashboardRefreshSeconds: 15, SecurityAlertThreshold: 1, ReadOnlyServerConfig: true, BackupAvailable: true}
	var raw string
	if err := a.store.DB().QueryRow(`SELECT value FROM admin_settings WHERE key='dashboard_refresh_seconds'`).Scan(&raw); err == nil {
		if v, e := strconv.Atoi(raw); e == nil && v >= 5 && v <= 120 {
			settings.DashboardRefreshSeconds = v
		}
	}
	if err := a.store.DB().QueryRow(`SELECT value FROM admin_settings WHERE key='security_alert_threshold'`).Scan(&raw); err == nil {
		if v, e := strconv.Atoi(raw); e == nil && v >= 0 && v <= 100000 {
			settings.SecurityAlertThreshold = v
		}
	}
	writeJSON(w, http.StatusOK, settings)
}

func (a *API) AdminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_settings")
	if !ok {
		return
	}
	var in struct {
		DashboardRefreshSeconds *int `json:"dashboard_refresh_seconds"`
		SecurityAlertThreshold  *int `json:"security_alert_threshold"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	updates := map[string]string{}
	if in.DashboardRefreshSeconds != nil {
		if *in.DashboardRefreshSeconds < 5 || *in.DashboardRefreshSeconds > 120 {
			writeError(w, 400, "dashboard refresh must be 5..120 seconds")
			return
		}
		updates["dashboard_refresh_seconds"] = strconv.Itoa(*in.DashboardRefreshSeconds)
	}
	if in.SecurityAlertThreshold != nil {
		if *in.SecurityAlertThreshold < 0 || *in.SecurityAlertThreshold > 100000 {
			writeError(w, 400, "invalid security alert threshold")
			return
		}
		updates["security_alert_threshold"] = strconv.Itoa(*in.SecurityAlertThreshold)
	}
	if len(updates) == 0 {
		writeError(w, 400, "no supported settings supplied")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to update settings")
		return
	}
	defer tx.Rollback()
	for k, v := range updates {
		if _, err := tx.Exec(`INSERT INTO admin_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, k, v, now); err != nil {
			writeError(w, 500, "failed to update settings")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "failed to update settings")
		return
	}
	a.adminAudit(admin.ID, "update_admin_settings", "", "keys="+strings.Join(sortedKeys(updates), ","))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updated": updates})
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	if len(out) == 2 && out[0] > out[1] {
		out[0], out[1] = out[1], out[0]
	}
	return out
}

func (a *API) AdminPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	writeJSON(w, http.StatusOK, AdminPrivacyPolicy{
		PasswordsExposed:             false,
		PrivateKeysExposed:           false,
		PrivateMessageBodiesExposed:  true,
		PrivateCallAudioExposed:      false,
		DecryptedCallMediaExposed:    false,
		PrivateFileContentsExposed:   false,
		AdministrativeActionsAudited: true,
		ServerSideRoleChecks:         true,
		MessageBodyAccessRole:        "owner",
		MessageBodyAccessAudited:     true,
	})
}
