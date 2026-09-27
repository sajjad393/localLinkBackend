package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	accountRecoveryTTL      = 30 * time.Minute
	recoveryCredentialTTL   = 10 * time.Minute
	maxRecoveryReasonLength = 256
)

type AccountRecoveryRequest struct {
	RequestID      string `json:"request_id"`
	Status         string `json:"status"`
	Username       string `json:"username"`
	ExpiresAt      string `json:"expires_at"`
	ServerID       string `json:"server_id"`
	Fingerprint    string `json:"fingerprint"`
	TargetDeviceID string `json:"target_device_id"`
}

type AccountRecoveryStatus struct {
	AccountRecoveryRequest
	ApprovedAt        string `json:"approved_at,omitempty"`
	RecoveryExpiresAt string `json:"recovery_expires_at,omitempty"`
	RejectedReason    string `json:"rejected_reason,omitempty"`
}

// AccountRecoveryState is kept as a compatibility name for the status response type.
type AccountRecoveryState = AccountRecoveryStatus

type RecoveryCodeStatusResponse struct {
	Configured bool   `json:"configured"`
	CreatedAt  string `json:"created_at,omitempty"`
	RotatedAt  string `json:"rotated_at,omitempty"`
}

func (a *API) CreateAccountRecovery(w http.ResponseWriter, r *http.Request) {
	if !a.allowSecurityAction("recovery-request", registrationKey(r), maxSensitiveAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "recovery request rate limit exceeded")
		return
	}

	var in struct {
		Username          string `json:"username"`
		Password          string `json:"password"`
		RecoveryCode      string `json:"recovery_code"`
		DeviceID          string `json:"device_id"`
		DeviceName        string `json:"device_name"`
		Platform          string `json:"platform"`
		IdentityPublicKey string `json:"identity_public_key"`
	}
	if err := decodeJSON(r, &in, 12<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	username := strings.ToLower(strings.TrimSpace(in.Username))
	in.RecoveryCode = normalizeRecoveryCode(in.RecoveryCode)
	deviceID := strings.TrimSpace(in.DeviceID)
	deviceName := strings.TrimSpace(in.DeviceName)
	platform := strings.TrimSpace(in.Platform)
	identityPublicKey := strings.TrimSpace(in.IdentityPublicKey)
	if !validUsername(username) || (in.Password == "" && in.RecoveryCode == "") || len(in.Password) > maxPasswordLen || hasControlChars(in.Password) || len(in.RecoveryCode) > 128 || hasControlChars(in.RecoveryCode) || deviceID == "" || len(deviceID) > 128 || hasControlChars(deviceID) || deviceName == "" || len(deviceName) > maxDeviceName || hasControlChars(deviceName) || !validIdentityPublicKey(identityPublicKey) {
		writeError(w, http.StatusBadRequest, "invalid recovery request data")
		return
	}
	if platform == "" {
		platform = "android"
	}
	if !a.allowPersistentSecurityAction("recovery-request-account", username, 6, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "recovery request rate limit exceeded")
		return
	}

	var userID, saltB64, hashB64, recoveryHash string
	var iterations int
	var accountStatus string
	var recoveryEnabled int
	if err := a.store.DB().QueryRow(`SELECT id,password_salt,password_hash,password_iterations,status,COALESCE(recovery_code_hash,''),COALESCE(recovery_code_enabled,1) FROM users WHERE username=?`, username).Scan(&userID, &saltB64, &hashB64, &iterations, &accountStatus, &recoveryHash, &recoveryEnabled); err != nil {
		// Keep the unknown-account path close to the existing password-cost path
		// so username enumeration does not become a cheap timing oracle.
		if in.Password != "" {
			_ = derivePasswordKey(in.Password, []byte("locallink-recovery-dummy-salt-v1"), passwordIterations)
		}
		writeError(w, http.StatusUnauthorized, "invalid recovery credentials")
		return
	}
	if accountStatus != "active" {
		writeError(w, http.StatusForbidden, "account is not active")
		return
	}
	authorized := false
	if in.RecoveryCode != "" && recoveryHash != "" && recoveryEnabled != 0 {
		authorized = secureTokenEqual(recoveryHash, tokenHash(in.RecoveryCode))
	}
	if !authorized && in.Password != "" {
		salt, err := base64.RawURLEncoding.DecodeString(saltB64)
		expected, decErr := base64.RawURLEncoding.DecodeString(hashB64)
		if err == nil && decErr == nil && len(salt) == passwordSaltLength && len(expected) == passwordKeyLength {
			authorized = hmac.Equal(derivePasswordKey(in.Password, salt, iterations), expected)
		}
	}
	if !authorized {
		writeError(w, http.StatusUnauthorized, "invalid recovery credentials")
		return
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(accountRecoveryTTL)
	_, _ = a.store.DB().Exec(`UPDATE account_recovery_requests SET status='expired',completed_at=? WHERE user_id=? AND status IN ('pending','approved')`, nowText, userID)

	requestSecret, err := newRecoverySecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recovery request")
		return
	}
	requestID := uuid.NewString()
	_, err = a.store.DB().Exec(`INSERT INTO account_recovery_requests(id,user_id,target_device_id,target_device_name,target_platform,target_identity_public_key,request_secret_hash,status,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		requestID, userID, deviceID, deviceName, platform, identityPublicKey, tokenHash(requestSecret), "pending", nowText, expires.Format(time.RFC3339Nano))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save recovery request")
		return
	}

	serverID, fingerprint := a.currentServerIdentity()
	a.audit("account_recovery_request", userID, "target="+deviceID)
	a.auditSecurity("account_recovery_request", userID, deviceID, registrationKey(r), "recovery request created")
	writeJSON(w, http.StatusAccepted, map[string]any{
		"request": AccountRecoveryRequest{
			RequestID: requestID, Status: "pending", Username: username,
			ExpiresAt: expires.Format(time.RFC3339Nano), ServerID: serverID,
			Fingerprint: fingerprint, TargetDeviceID: deviceID,
		},
		"request_secret":  requestSecret,
		"poll_expires_at": expires.Format(time.RFC3339Nano),
	})
}

func (a *API) AccountRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.allowSecurityAction("recovery-status", registrationKey(r), maxSensitiveAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "recovery status rate limit exceeded")
		return
	}
	var in struct {
		RequestID     string `json:"request_id"`
		RequestSecret string `json:"request_secret"`
	}
	if err := decodeJSON(r, &in, 4<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid recovery status request")
		return
	}
	requestID := strings.TrimSpace(in.RequestID)
	requestSecret := strings.TrimSpace(in.RequestSecret)
	if requestID == "" || len(requestID) > 128 || hasControlChars(requestID) || requestSecret == "" || len(requestSecret) > 256 || hasControlChars(requestSecret) {
		writeError(w, http.StatusBadRequest, "request id and request secret are required")
		return
	}
	if !a.allowPersistentSecurityAction("recovery-status-request", requestID, 30, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "recovery status rate limit exceeded")
		return
	}
	var targetDeviceID, status, expires, username, rejectedReason, approvedAt, recoveryExpiresAt string
	err := a.store.DB().QueryRow(`SELECT r.target_device_id,r.status,r.expires_at,u.username,COALESCE(r.rejected_reason,''),COALESCE(r.approved_at,''),COALESCE(r.recovery_expires_at,'') FROM account_recovery_requests r JOIN users u ON u.id=r.user_id WHERE r.id=? AND r.request_secret_hash=?`, requestID, tokenHash(requestSecret)).Scan(&targetDeviceID, &status, &expires, &username, &rejectedReason, &approvedAt, &recoveryExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recovery request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recovery request")
		return
	}
	now := time.Now().UTC()
	if expiry, parseErr := time.Parse(time.RFC3339Nano, expires); parseErr != nil || !now.Before(expiry) {
		_, _ = a.store.DB().Exec(`UPDATE account_recovery_requests SET status='expired',completed_at=?,recovery_credential_hash=NULL,recovery_expires_at=NULL WHERE id=? AND status IN ('pending','approved')`, now.Format(time.RFC3339Nano), requestID)
		status = "expired"
		recoveryExpiresAt = ""
	}
	if status == "approved" && recoveryExpiresAt != "" {
		recoveryExpiry, parseErr := time.Parse(time.RFC3339Nano, recoveryExpiresAt)
		if parseErr != nil || !now.Before(recoveryExpiry) {
			_, _ = a.store.DB().Exec(`UPDATE account_recovery_requests SET recovery_credential_hash=NULL,recovery_expires_at=NULL,status='expired',completed_at=? WHERE id=? AND status='approved'`, now.Format(time.RFC3339Nano), requestID)
			status = "expired"
			recoveryExpiresAt = ""
		}
	}
	writeJSON(w, http.StatusOK, AccountRecoveryState{AccountRecoveryRequest: AccountRecoveryRequest{RequestID: requestID, Status: status, Username: username, ExpiresAt: expires, TargetDeviceID: targetDeviceID}, ApprovedAt: approvedAt, RecoveryExpiresAt: recoveryExpiresAt, RejectedReason: rejectedReason})
}

func (a *API) CompleteAccountRecovery(w http.ResponseWriter, r *http.Request) {
	if !a.allowSecurityAction("recovery-complete", registrationKey(r), maxSensitiveAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "recovery completion rate limit exceeded")
		return
	}
	var in struct {
		RequestID          string `json:"request_id"`
		RequestSecret      string `json:"request_secret"`
		RecoveryCredential string `json:"recovery_credential"`
		DeviceID           string `json:"device_id"`
		DeviceName         string `json:"device_name"`
		Platform           string `json:"platform"`
		IdentityPublicKey  string `json:"identity_public_key"`
	}
	if err := decodeJSON(r, &in, 12<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.RequestID = strings.TrimSpace(in.RequestID)
	in.RequestSecret = strings.TrimSpace(in.RequestSecret)
	in.RecoveryCredential = strings.TrimSpace(in.RecoveryCredential)
	in.DeviceID = strings.TrimSpace(in.DeviceID)
	in.DeviceName = strings.TrimSpace(in.DeviceName)
	in.Platform = strings.TrimSpace(in.Platform)
	in.IdentityPublicKey = strings.TrimSpace(in.IdentityPublicKey)
	if in.RequestID == "" || len(in.RequestID) > 128 || hasControlChars(in.RequestID) || in.RequestSecret == "" || len(in.RequestSecret) > 256 || in.RecoveryCredential == "" || len(in.RecoveryCredential) > 256 || in.DeviceID == "" || len(in.DeviceID) > 128 || hasControlChars(in.DeviceID) || in.DeviceName == "" || len(in.DeviceName) > maxDeviceName || hasControlChars(in.DeviceName) || !validIdentityPublicKey(in.IdentityPublicKey) {
		writeError(w, http.StatusBadRequest, "invalid recovery completion data")
		return
	}
	if !a.allowPersistentSecurityAction("recovery-complete-request", in.RequestID, 20, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "recovery completion rate limit exceeded")
		return
	}
	if in.Platform == "" {
		in.Platform = "android"
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start recovery transaction")
		return
	}
	defer tx.Rollback()

	var userID, targetDeviceID, requestStatus, requestExpires, recoveryExpires, recoveryHash, requestedIdentityKey, username string
	err = tx.QueryRow(`SELECT r.user_id,r.target_device_id,r.status,r.expires_at,COALESCE(r.recovery_expires_at,''),COALESCE(r.recovery_credential_hash,''),r.target_identity_public_key,u.username FROM account_recovery_requests r JOIN users u ON u.id=r.user_id WHERE r.id=? AND r.request_secret_hash=?`, in.RequestID, tokenHash(in.RequestSecret)).Scan(&userID, &targetDeviceID, &requestStatus, &requestExpires, &recoveryExpires, &recoveryHash, &requestedIdentityKey, &username)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recovery request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recovery request")
		return
	}
	if requestStatus != "approved" {
		writeError(w, http.StatusConflict, "recovery request is not approved")
		return
	}
	requestExpiry, err := time.Parse(time.RFC3339Nano, requestExpires)
	if err != nil || !now.Before(requestExpiry) {
		_, _ = tx.Exec(`UPDATE account_recovery_requests SET status='expired',completed_at=?,recovery_credential_hash=NULL,recovery_expires_at=NULL WHERE id=? AND status='approved'`, nowText, in.RequestID)
		writeError(w, http.StatusGone, "recovery request has expired")
		return
	}
	recoveryExpiry, err := time.Parse(time.RFC3339Nano, recoveryExpires)
	if err != nil || !now.Before(recoveryExpiry) {
		_, _ = tx.Exec(`UPDATE account_recovery_requests SET recovery_credential_hash=NULL,recovery_expires_at=NULL,status='expired',completed_at=? WHERE id=? AND status='approved'`, nowText, in.RequestID)
		writeError(w, http.StatusGone, "recovery credential has expired")
		return
	}
	if recoveryHash == "" || !secureTokenEqual(recoveryHash, tokenHash(in.RecoveryCredential)) {
		writeError(w, http.StatusUnauthorized, "invalid recovery credential")
		return
	}
	if targetDeviceID != in.DeviceID {
		writeError(w, http.StatusConflict, "device does not match the recovery request")
		return
	}
	if requestedIdentityKey != in.IdentityPublicKey {
		writeError(w, http.StatusConflict, "identity key does not match the recovery request")
		return
	}
	var accountStatus string
	if err := tx.QueryRow(`SELECT status FROM users WHERE id=?`, userID).Scan(&accountStatus); err != nil || accountStatus != "active" {
		writeError(w, http.StatusForbidden, "account is not active")
		return
	}
	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM devices WHERE id=?`, in.DeviceID).Scan(&existing); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect target device")
		return
	}
	if existing != 0 {
		writeError(w, http.StatusConflict, "target device id is already registered")
		return
	}

	token, err := newToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate recovery session")
		return
	}
	sessionID := uuid.NewString()
	sessionExpires := now.Add(authSessionTTL)
	if _, err := tx.Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash,status,identity_public_key,user_id) VALUES(?,?,?,?,?,?, 'active', ?,?)`, in.DeviceID, in.DeviceName, in.Platform, nowText, nowText, tokenHash(token), in.IdentityPublicKey, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to register recovered device")
		return
	}
	if err := ensureIdentityKeyHistory(tx, in.DeviceID, in.IdentityPublicKey, 1, nowText); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize recovered device identity history")
		return
	}
	if _, err := tx.Exec(`INSERT INTO auth_sessions(id,user_id,device_id,token_hash,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?)`, sessionID, userID, in.DeviceID, tokenHash(token), nowText, nowText, sessionExpires.Format(time.RFC3339Nano)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create recovered session")
		return
	}

	oldDevices, err := revokeOtherDevicesAndMigrateGroups(tx, userID, in.DeviceID, nowText)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to replace lost device")
		return
	}
	result, err := tx.Exec(`UPDATE account_recovery_requests SET status='completed',completed_at=?,recovery_credential_hash=NULL,recovery_expires_at=NULL WHERE id=? AND status='approved'`, nowText, in.RequestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finalize recovery request")
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		writeError(w, http.StatusConflict, "recovery request was already completed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit account recovery")
		return
	}
	for _, oldID := range oldDevices {
		a.hub.CloseDevice(oldID)
		d, _ := a.setPresence(oldID, false, false, DevicePresence{})
		a.broadcastPresence(oldID, d)
	}

	var account Account
	if err := a.store.DB().QueryRow(`SELECT id,username,created_at FROM users WHERE id=?`, userID).Scan(&account.ID, &account.Username, &account.CreatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recovered account")
		return
	}
	profile := profileForUserDB(a.store.DB(), userID, username)
	if profile.UserID == "" {
		writeError(w, http.StatusInternalServerError, "failed to read recovered profile")
		return
	}
	a.audit("account_recovery_complete", userID, "target="+in.DeviceID+" revoked_devices="+strings.Join(oldDevices, ","))
	a.auditSecurity("account_recovery_complete", userID, in.DeviceID, registrationKey(r), "recovery completed")
	writeJSON(w, http.StatusOK, AccountTransferResponse{
		Account: account,
		Device:  Device{ID: in.DeviceID, Name: in.DeviceName, Platform: in.Platform, CreatedAt: nowText, LastSeenAt: nowText, Status: "active"},
		Profile: profile, Token: token, ExpiresAt: sessionExpires.Format(time.RFC3339Nano),
		SourceDeviceID: strings.Join(oldDevices, ","), SourceDeviceRevoked: len(oldDevices) > 0,
	})
}

func (a *API) AdminRecoveryRequests(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-recovery-requests")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	args := []any{}
	where := ""
	if params.Cursor != nil {
		where = `WHERE (r.created_at < ? OR (r.created_at=? AND r.id<?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT r.id,u.username,r.status,r.created_at,r.expires_at,r.target_device_id,r.target_device_name,r.target_platform,COALESCE(r.approved_at,''),COALESCE(r.rejected_at,''),COALESCE(r.rejected_reason,''),COALESCE(r.recovery_expires_at,'') FROM account_recovery_requests r JOIN users u ON u.id=r.user_id `+where+` ORDER BY r.created_at DESC,r.id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list recovery requests")
		return
	}
	defer rows.Close()
	type row struct {
		ID                string `json:"id"`
		Username          string `json:"username"`
		Status            string `json:"status"`
		CreatedAt         string `json:"created_at"`
		ExpiresAt         string `json:"expires_at"`
		TargetDeviceID    string `json:"target_device_id"`
		TargetDeviceName  string `json:"target_device_name"`
		TargetPlatform    string `json:"target_platform"`
		ApprovedAt        string `json:"approved_at,omitempty"`
		RejectedAt        string `json:"rejected_at,omitempty"`
		RejectedReason    string `json:"rejected_reason,omitempty"`
		RecoveryExpiresAt string `json:"recovery_expires_at,omitempty"`
	}
	out := make([]row, 0, params.Limit)
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.ID, &x.Username, &x.Status, &x.CreatedAt, &x.ExpiresAt, &x.TargetDeviceID, &x.TargetDeviceName, &x.TargetPlatform, &x.ApprovedAt, &x.RejectedAt, &x.RejectedReason, &x.RecoveryExpiresAt); err != nil {
			writeError(w, 500, "failed to read recovery requests")
			return
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read recovery requests")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		RecoveryRequests []row `json:"recovery_requests"`
		PageInfo
	}{RecoveryRequests: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-recovery-requests", last.CreatedAt, last.ID, true)
	}
	writeJSON(w, 200, resp)
}

func (a *API) AdminApproveRecovery(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "manage_recovery"); !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || len(id) > 128 || hasControlChars(id) {
		writeError(w, http.StatusBadRequest, "recovery request id required")
		return
	}
	credential, err := newRecoverySecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate recovery credential")
		return
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(recoveryCredentialTTL).Format(time.RFC3339Nano)
	res, err := a.store.DB().Exec(`UPDATE account_recovery_requests SET status='approved',approved_at=?,recovery_expires_at=?,recovery_credential_hash=?,rejected_at=NULL,rejected_reason='' WHERE id=? AND status='pending' AND expires_at>?`, nowText, expires, tokenHash(credential), id, nowText)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve recovery request")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusConflict, "recovery request is no longer pending or has expired")
		return
	}
	var userID string
	_ = a.store.DB().QueryRow(`SELECT user_id FROM account_recovery_requests WHERE id=?`, id).Scan(&userID)
	a.audit("account_recovery_approve", userID, "request="+id)
	a.auditSecurity("account_recovery_approve", userID, "", registrationKey(r), "admin approved recovery")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "approved": true, "recovery_credential": credential, "expires_at": expires})
}

func (a *API) AdminReissueRecovery(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "manage_recovery"); !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || len(id) > 128 || hasControlChars(id) {
		writeError(w, http.StatusBadRequest, "recovery request id required")
		return
	}
	credential, err := newRecoverySecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate recovery credential")
		return
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(recoveryCredentialTTL).Format(time.RFC3339Nano)
	res, err := a.store.DB().Exec(`UPDATE account_recovery_requests SET recovery_expires_at=?,recovery_credential_hash=? WHERE id=? AND status='approved' AND expires_at>?`, expires, tokenHash(credential), id, nowText)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reissue recovery credential")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusConflict, "recovery request is not approved or has expired")
		return
	}
	var userID string
	_ = a.store.DB().QueryRow(`SELECT user_id FROM account_recovery_requests WHERE id=?`, id).Scan(&userID)
	a.audit("account_recovery_reissue", userID, "request="+id)
	a.auditSecurity("account_recovery_reissue", userID, "", registrationKey(r), "admin reissued recovery credential")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recovery_credential": credential, "expires_at": expires})
}

func (a *API) AdminRejectRecovery(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "manage_recovery"); !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	if id == "" || len(id) > 128 || hasControlChars(id) || len(reason) > maxRecoveryReasonLength || hasControlChars(reason) {
		writeError(w, http.StatusBadRequest, "invalid recovery request or reason")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := a.store.DB().Exec(`UPDATE account_recovery_requests SET status='rejected',rejected_at=?,rejected_reason=?,recovery_credential_hash=NULL,recovery_expires_at=NULL WHERE id=? AND status='pending'`, now, reason, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reject recovery request")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusConflict, "recovery request is no longer pending")
		return
	}
	var userID string
	_ = a.store.DB().QueryRow(`SELECT user_id FROM account_recovery_requests WHERE id=?`, id).Scan(&userID)
	a.audit("account_recovery_reject", userID, "request="+id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rejected": true})
}

func (a *API) currentServerIdentity() (string, string) {
	var secret, serverID string
	if err := a.store.DB().QueryRow(`SELECT server_id,identity_secret FROM server_identity WHERE singleton=1`).Scan(&serverID, &secret); err != nil {
		return "", ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(decoded) != 32 {
		return serverID, ""
	}
	return serverID, serverFingerprint(decoded)
}

func newRecoverySecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func revokeOtherDevicesAndMigrateGroups(tx *sql.Tx, userID, targetID, nowText string) ([]string, error) {
	rows, err := tx.Query(`SELECT id FROM devices WHERE user_id=? AND id<>? AND COALESCE(status,'active')='active'`, userID, targetID)
	if err != nil {
		return nil, err
	}
	var oldIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		oldIDs = append(oldIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(oldIDs) == 0 {
		return oldIDs, nil
	}
	if err := migrateAllUserGroups(tx, oldIDs, targetID, userID); err != nil {
		return nil, err
	}
	for _, oldID := range oldIDs {
		if _, err := tx.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, nowText, oldID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`UPDATE devices SET status='revoked',revoked_at=?,token_hash=NULL WHERE id=? AND user_id=? AND COALESCE(status,'active')='active'`, nowText, oldID, userID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`UPDATE device_identity_keys SET retired_at=? WHERE device_id=? AND retired_at IS NULL`, nowText, oldID); err != nil {
			return nil, err
		}
	}
	return oldIDs, nil
}

func migrateAllUserGroups(tx *sql.Tx, oldIDs []string, targetID, userID string) error {
	placeholders := make([]string, len(oldIDs))
	args := make([]any, 0, len(oldIDs))
	for i, id := range oldIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := tx.Query(`SELECT group_id,role,joined_at FROM group_members WHERE device_id IN (`+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return err
	}
	type membership struct{ groupID, role, joinedAt string }
	chosen := map[string]membership{}
	for rows.Next() {
		var m membership
		if err := rows.Scan(&m.groupID, &m.role, &m.joinedAt); err != nil {
			rows.Close()
			return err
		}
		prev, ok := chosen[m.groupID]
		if !ok || roleRank(m.role) > roleRank(prev.role) {
			chosen[m.groupID] = m
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, m := range chosen {
		if _, err := tx.Exec(`INSERT INTO group_members(group_id,device_id,role,joined_at) VALUES(?,?,?,?) ON CONFLICT(group_id,device_id) DO UPDATE SET role=excluded.role`, m.groupID, targetID, m.role, m.joinedAt); err != nil {
			return err
		}
	}
	ownerArgs := make([]any, 0, len(oldIDs)+3)
	ownerArgs = append(ownerArgs, targetID)
	for _, id := range oldIDs {
		ownerArgs = append(ownerArgs, id)
	}
	ownerArgs = append(ownerArgs, targetID, userID)
	ownerQuery := `UPDATE groups SET owner_id=? WHERE owner_id IN (` + strings.Join(placeholders, ",") + `) AND EXISTS (SELECT 1 FROM devices d WHERE d.id=? AND d.user_id=?)`
	if _, err := tx.Exec(ownerQuery, ownerArgs...); err != nil {
		return err
	}
	for _, oldID := range oldIDs {
		if _, err := tx.Exec(`DELETE FROM group_members WHERE device_id=?`, oldID); err != nil {
			return err
		}
	}
	return nil
}

func roleRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "owner":
		return 3
	case "admin":
		return 2
	default:
		return 1
	}
}

// RecoveryCodeStatus reports whether the authenticated account has a backup recovery code.
// The code itself is never returned.
func (a *API) RecoveryCodeStatus(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var userID string
	if err := a.store.DB().QueryRow(`SELECT user_id FROM devices WHERE id=? AND status='active'`, deviceID).Scan(&userID); err != nil || userID == "" {
		writeError(w, http.StatusNotFound, "account is not linked to this device")
		return
	}
	var hash, createdAt, rotatedAt string
	var enabled int
	if err := a.store.DB().QueryRow(`SELECT COALESCE(recovery_code_hash,''),COALESCE(recovery_code_created_at,''),COALESCE(recovery_code_rotated_at,''),COALESCE(recovery_code_enabled,1) FROM users WHERE id=? AND status='active'`, userID).Scan(&hash, &createdAt, &rotatedAt, &enabled); err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	writeJSON(w, http.StatusOK, RecoveryCodeStatusResponse{Configured: hash != "" && enabled != 0, CreatedAt: createdAt, RotatedAt: rotatedAt})
}

// RotateRecoveryCode creates a new backup recovery code and invalidates the previous one.
// The plaintext code is returned only in this response and is never stored.
func (a *API) RotateRecoveryCode(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !a.allowSecurityAction("recovery-code-rotate", deviceID, maxSensitiveAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "recovery code rotation rate limit exceeded")
		return
	}
	var userID string
	if err := a.store.DB().QueryRow(`SELECT user_id FROM devices WHERE id=? AND status='active'`, deviceID).Scan(&userID); err != nil || userID == "" {
		writeError(w, http.StatusNotFound, "account is not linked to this device")
		return
	}
	code, err := newRecoveryCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate recovery code")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := a.store.DB().Exec(`UPDATE users SET recovery_code_hash=?,recovery_code_created_at=COALESCE(NULLIF(recovery_code_created_at,''),?),recovery_code_rotated_at=?,recovery_code_enabled=1,updated_at=? WHERE id=? AND status='active'`, tokenHash(code), now, now, now, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save recovery code")
		return
	}
	a.audit("recovery_code_rotate", userID, "device="+deviceID)
	a.auditSecurity("recovery_code_rotate", userID, deviceID, registrationKey(r), "backup recovery code rotated")
	writeJSON(w, http.StatusOK, map[string]any{"recovery_code": code})
}

// DisableRecoveryCode removes the permanent backup recovery code without affecting
// the account password or already-approved recovery requests. A later rotation
// generates a fresh code and re-enables the feature.
func (a *API) DisableRecoveryCode(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !a.allowSecurityAction("recovery-code-disable", deviceID, maxSensitiveAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "recovery code disable rate limit exceeded")
		return
	}
	var userID string
	if err := a.store.DB().QueryRow(`SELECT user_id FROM devices WHERE id=? AND status='active'`, deviceID).Scan(&userID); err != nil || userID == "" {
		writeError(w, http.StatusNotFound, "account is not linked to this device")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := a.store.DB().Exec(`UPDATE users SET recovery_code_hash=NULL,recovery_code_enabled=0,recovery_code_rotated_at=?,updated_at=? WHERE id=? AND status='active'`, now, now, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable recovery code")
		return
	}
	a.audit("recovery_code_disable", userID, "device="+deviceID)
	a.auditSecurity("recovery_code_disable", userID, deviceID, registrationKey(r), "backup recovery code disabled")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func normalizeRecoveryCode(value string) string {
	raw := strings.ToUpper(strings.TrimSpace(value))
	raw = strings.NewReplacer("-", "", " ", "", "\t", "", "\r", "", "\n", "").Replace(raw)
	if len(raw) != 20 {
		return strings.TrimSpace(value)
	}
	return raw[:5] + "-" + raw[5:10] + "-" + raw[10:15] + "-" + raw[15:20]
}
