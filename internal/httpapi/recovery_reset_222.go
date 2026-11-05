package httpapi

// Bundle 222: "Forgot password?" with the user's own recovery code.
//
//   - POST /api/v1/account/recovery/reset
//     username + recovery code + new password -> the password is changed at
//     once (no admin), THIS phone is signed in, a NEW recovery code is shown
//     once, and the user's other phones stay signed in.
//   - The same route also takes a ready password hash (new_password_hash +
//     new_password_salt + iterations) when a phone reset the password offline
//     and now brings it to the server. The real password never travels then.
//   - The no-code path (CreateAccountRecovery no_code + admin approval +
//     CompleteAccountRecovery new_password) finishes with the same helper.

import (
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
	recoveryKindMove      = "move"
	recoveryKindNoCode    = "no_code"
	recoveryKindResetCode = "reset_code"
	// A no-code request waits until the admin answers (no time limit).
	noCodeRequestTTL = 10 * 365 * 24 * time.Hour
	// The admin's short code (given in person) lives one day; Reissue makes a new one.
	noCodeCredentialTTL = 24 * time.Hour
	minResetIterations  = 1000
	maxResetIterations  = 2000000
)

type passwordReset222 struct {
	UserID, Username                   string
	SaltB64, HashB64                   string
	Iterations                         int
	DeviceID, DeviceName, Platform     string
	HardwareFP, IdentityPublicKey      string
	Kind                               string
	NowText                            string
	sessionToken, sessionExpires, code string
}

// newShortRecoveryCode222 makes the admin's one-time code for a no-code
// request: 12 digits, "1234-5678-9012", easy to read out in person.
func newShortRecoveryCode222() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	d := make([]byte, 0, 14)
	for i, v := range b {
		if i == 4 || i == 8 {
			d = append(d, '-')
		}
		d = append(d, '0'+v%10)
	}
	return string(d), nil
}

// digitsOnly222 keeps the digits of a typed short code ("1234 5678-9012").
func digitsOnly222(value string) string {
	var sb strings.Builder
	for _, c := range value {
		if c >= '0' && c <= '9' {
			sb.WriteRune(c)
		}
	}
	return sb.String()
}

// checkResetDevice222 refuses a phone id that belongs to someone else or was
// revoked. A phone of the same user (the usual "forgot password" case) is fine.
func (a *API) checkResetDevice222(w http.ResponseWriter, userID, deviceID, hardwareFP string) bool {
	var owner sql.NullString
	var status string
	err := a.store.DB().QueryRow(`SELECT user_id,COALESCE(status,'active') FROM devices WHERE id=?`, deviceID).Scan(&owner, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect device")
		return false
	}
	if status != "active" {
		writeError(w, http.StatusForbidden, "device is revoked and cannot re-authenticate")
		return false
	}
	if !owner.Valid || owner.String != userID {
		var name, fp string
		_ = a.store.DB().QueryRow(`SELECT COALESCE(u.username,''),COALESCE(d.hardware_fp,'') FROM devices d LEFT JOIN users u ON u.id=d.user_id WHERE d.id=?`, deviceID).Scan(&name, &fp)
		writeDeviceTaken218(w, "device id belongs to another or legacy device", name, fp, hardwareFP)
		return false
	}
	return true
}

// applyPasswordReset222 runs inside tx: new password, new recovery code, this
// phone signed in (same device/session steps as LoginAccount). Other phones
// of the user are NOT touched.
func applyPasswordReset222(tx *sql.Tx, p *passwordReset222, expires time.Time) error {
	code, err := newRecoveryCode()
	if err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE users SET password_salt=?,password_hash=?,password_iterations=?,recovery_code_hash=?,recovery_code_rotated_at=?,recovery_code_enabled=1,updated_at=? WHERE id=? AND status='active'`,
		p.SaltB64, p.HashB64, p.Iterations, tokenHash(normalizeRecoveryCode(code)), p.NowText, p.NowText, p.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("account is not active")
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	var owner sql.NullString
	devErr := tx.QueryRow(`SELECT user_id FROM devices WHERE id=?`, p.DeviceID).Scan(&owner)
	if devErr == nil {
		if _, err := tx.Exec(`UPDATE devices SET name=?,platform=?,last_seen_at=?,token_hash=?,hardware_fp=CASE WHEN ?='' THEN hardware_fp ELSE ? END WHERE id=? AND user_id=?`,
			p.DeviceName, p.Platform, p.NowText, tokenHash(token), p.HardwareFP, p.HardwareFP, p.DeviceID, p.UserID); err != nil {
			return err
		}
		_, _ = tx.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, p.NowText, p.DeviceID)
	} else if errors.Is(devErr, sql.ErrNoRows) {
		if _, err := tx.Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash,user_id,hardware_fp,identity_public_key) VALUES(?,?,?,?,?,?,?,?,?)`,
			p.DeviceID, p.DeviceName, p.Platform, p.NowText, p.NowText, tokenHash(token), p.UserID, p.HardwareFP, p.IdentityPublicKey); err != nil {
			return err
		}
		if p.IdentityPublicKey != "" {
			if err := ensureIdentityKeyHistory(tx, p.DeviceID, p.IdentityPublicKey, 1, p.NowText); err != nil {
				return err
			}
		}
	} else {
		return devErr
	}
	if _, err := tx.Exec(`INSERT INTO auth_sessions(id,user_id,device_id,token_hash,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?)`,
		uuid.NewString(), p.UserID, p.DeviceID, tokenHash(token), p.NowText, p.NowText, expires.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO profiles(user_id,display_name,avatar_path,avatar_content_type,avatar_size,avatar_sha256,avatar_updated_at,created_at,updated_at) VALUES(?,?,NULL,NULL,0,'',NULL,?,?) ON CONFLICT(user_id) DO NOTHING`,
		p.UserID, p.Username, p.NowText, p.NowText); err != nil {
		return err
	}
	p.sessionToken, p.sessionExpires, p.code = token, expires.Format(time.RFC3339Nano), code
	return nil
}

// ResetPasswordWithCode222 is POST /api/v1/account/recovery/reset.
func (a *API) ResetPasswordWithCode222(w http.ResponseWriter, r *http.Request) {
	if !a.allowSecurityAction("recovery-reset-ip", registrationKey(r), maxLoginAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeErrorCode(w, http.StatusTooManyRequests, "RATE_LIMIT", "too many tries")
		return
	}
	var in struct {
		Username        string `json:"username"`
		RecoveryCode    string `json:"recovery_code"`
		NewPassword     string `json:"new_password"`
		NewPasswordHash string `json:"new_password_hash"`
		NewPasswordSalt string `json:"new_password_salt"`
		Iterations      int    `json:"iterations"`
		DeviceID        string `json:"device_id"`
		DeviceName      string `json:"device_name"`
		Platform        string `json:"platform"`
		HardwareFP      string `json:"hardware_fp"`
		PairingCode     string `json:"pairing_code"`
		IdentityKey     string `json:"identity_public_key"`
	}
	if err := decodeJSON(r, &in, 12<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	code := normalizeRecoveryCode(in.RecoveryCode)
	deviceID := strings.TrimSpace(in.DeviceID)
	deviceName := strings.TrimSpace(in.DeviceName)
	platform := strings.TrimSpace(in.Platform)
	hardwareFP := strings.TrimSpace(in.HardwareFP)
	identityKey := strings.TrimSpace(in.IdentityKey)
	if len(hardwareFP) > 128 || hasControlChars(hardwareFP) {
		hardwareFP = ""
	}
	if identityKey != "" && !validIdentityPublicKey(identityKey) {
		identityKey = ""
	}
	byHash := in.NewPasswordHash != "" || in.NewPasswordSalt != ""
	bad := !validUsername(username) || code == "" || len(code) > 128 || hasControlChars(code) ||
		deviceID == "" || len(deviceID) > 128 || hasControlChars(deviceID) ||
		deviceName == "" || len(deviceName) > maxDeviceName || hasControlChars(deviceName)
	var saltB64, hashB64 string
	iterations := passwordIterations
	if byHash {
		salt, e1 := base64.RawURLEncoding.DecodeString(strings.TrimSpace(in.NewPasswordSalt))
		hash, e2 := base64.RawURLEncoding.DecodeString(strings.TrimSpace(in.NewPasswordHash))
		if e1 != nil || e2 != nil || len(salt) != passwordSaltLength || len(hash) != passwordKeyLength || in.Iterations < minResetIterations || in.Iterations > maxResetIterations {
			bad = true
		} else {
			saltB64, hashB64, iterations = base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(hash), in.Iterations
		}
	} else if len(in.NewPassword) < 8 || len(in.NewPassword) > maxPasswordLen || hasControlChars(in.NewPassword) {
		bad = true
	}
	if bad {
		writeErrorCode(w, http.StatusBadRequest, "BAD_RESET_DATA", "invalid password reset data")
		return
	}
	if platform == "" {
		platform = "android"
	}
	if !a.allowPersistentSecurityAction("recovery-reset", username, 5, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeErrorCode(w, http.StatusTooManyRequests, "RATE_LIMIT", "too many tries")
		return
	}
	var userID, status, recoveryHash string
	var enabled int
	err := a.store.DB().QueryRow(`SELECT id,status,COALESCE(recovery_code_hash,''),COALESCE(recovery_code_enabled,1) FROM users WHERE username=?`, username).Scan(&userID, &status, &recoveryHash, &enabled)
	if err != nil || recoveryHash == "" || enabled == 0 || !secureTokenEqual(recoveryHash, tokenHash(code)) {
		if !byHash {
			// Same cost as a real reset so a wrong name is not faster.
			_ = derivePasswordKey(in.NewPassword, []byte("locallink-recovery-dummy-salt-v1"), passwordIterations)
		}
		a.auditSecurity("password_reset_failed", userID, deviceID, registrationKey(r), "bad name or recovery code")
		writeErrorCode(w, http.StatusUnauthorized, "BAD_RECOVERY_CODE", "username or recovery code is wrong")
		return
	}
	if status != "active" {
		writeErrorCode(w, http.StatusForbidden, "NOT_ACTIVE", "account is not active")
		return
	}
	if !a.checkResetDevice222(w, userID, deviceID, hardwareFP) {
		return
	}
	var known int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM devices WHERE id=?`, deviceID).Scan(&known)
	if known == 0 {
		if required := strings.TrimSpace(getenv("LOCALLINK_PAIRING_CODE", "")); required != "" && !secureTokenEqual(required, strings.TrimSpace(in.PairingCode)) {
			writeErrorCode(w, http.StatusForbidden, "PAIRING_REQUIRED", "device pairing is required for this first login")
			return
		}
	}
	if !byHash {
		salt := make([]byte, passwordSaltLength)
		if _, err := rand.Read(salt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create password salt")
			return
		}
		saltB64 = base64.RawURLEncoding.EncodeToString(salt)
		hashB64 = base64.RawURLEncoding.EncodeToString(derivePasswordKey(in.NewPassword, salt, passwordIterations))
	}
	now := time.Now().UTC()
	p := &passwordReset222{UserID: userID, Username: username, SaltB64: saltB64, HashB64: hashB64, Iterations: iterations,
		DeviceID: deviceID, DeviceName: deviceName, Platform: platform, HardwareFP: hardwareFP, IdentityPublicKey: identityKey,
		Kind: recoveryKindResetCode, NowText: now.Format(time.RFC3339Nano)}
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start reset")
		return
	}
	defer tx.Rollback()
	if err := applyPasswordReset222(tx, p, now.Add(authSessionTTL)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset password")
		return
	}
	// A done row so the Owner sees it in Recovery ("Reset with code", audit only).
	secret, _ := newRecoverySecret()
	_, _ = tx.Exec(`INSERT INTO account_recovery_requests(id,user_id,target_device_id,target_device_name,target_platform,target_identity_public_key,request_secret_hash,status,created_at,expires_at,completed_at,kind) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), userID, deviceID, deviceName, platform, identityKey, tokenHash(secret), "completed", p.NowText, p.NowText, p.NowText, recoveryKindResetCode)
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit reset")
		return
	}
	a.resetLoginFailures(userID)
	via := "online"
	if byHash {
		via = "offline_sync"
	}
	a.audit("password_reset_with_code", userID, "device="+deviceID+" via="+via)
	a.auditSecurity("password_reset_with_code", userID, deviceID, registrationKey(r), "password reset with recovery code ("+via+")")
	var account Account
	_ = a.store.DB().QueryRow(`SELECT id,username,created_at FROM users WHERE id=?`, userID).Scan(&account.ID, &account.Username, &account.CreatedAt)
	writeJSON(w, http.StatusOK, AuthResponse{
		Account:      account,
		Device:       Device{ID: deviceID, Name: deviceName, Platform: platform},
		Token:        p.sessionToken,
		ExpiresAt:    p.sessionExpires,
		RecoveryCode: p.code,
	})
}

// newRecoveryCredential222 makes the admin's one-time code. A no-code request
// gets a short 12-digit code (read out in person, lives a day); the old
// "move my account" requests keep the long code and 10 minutes.
func (a *API) newRecoveryCredential222(requestID string) (credential, hash string, ttl time.Duration, err error) {
	var kind string
	_ = a.store.DB().QueryRow(`SELECT COALESCE(kind,'move') FROM account_recovery_requests WHERE id=?`, requestID).Scan(&kind)
	if kind == recoveryKindNoCode {
		credential, err = newShortRecoveryCode222()
		return credential, tokenHash(digitsOnly222(credential)), noCodeCredentialTTL, err
	}
	credential, err = newRecoverySecret()
	return credential, tokenHash(credential), recoveryCredentialTTL, err
}

// completeNoCode222 finishes an approved no-code request: the one-time code
// was already checked by CompleteAccountRecovery.
func (a *API) completeNoCode222(w http.ResponseWriter, r *http.Request, requestID, userID, username, newPassword, deviceID, deviceName, platform, hardwareFP, identityKey string) {
	if len(newPassword) < 8 || len(newPassword) > maxPasswordLen || hasControlChars(newPassword) {
		writeErrorCode(w, http.StatusBadRequest, "BAD_RESET_DATA", "new password is required")
		return
	}
	if len(hardwareFP) > 128 || hasControlChars(hardwareFP) {
		hardwareFP = ""
	}
	if !a.checkResetDevice222(w, userID, deviceID, hardwareFP) {
		return
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create password salt")
		return
	}
	now := time.Now().UTC()
	p := &passwordReset222{UserID: userID, Username: username,
		SaltB64: base64.RawURLEncoding.EncodeToString(salt), HashB64: base64.RawURLEncoding.EncodeToString(derivePasswordKey(newPassword, salt, passwordIterations)),
		Iterations: passwordIterations, DeviceID: deviceID, DeviceName: deviceName, Platform: platform, HardwareFP: hardwareFP,
		IdentityPublicKey: identityKey, Kind: recoveryKindNoCode, NowText: now.Format(time.RFC3339Nano)}
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start recovery transaction")
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE account_recovery_requests SET status='completed',completed_at=?,recovery_credential_hash=NULL,recovery_expires_at=NULL WHERE id=? AND status='approved'`, p.NowText, requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finalize recovery request")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		writeError(w, http.StatusConflict, "recovery request was already completed")
		return
	}
	if err := applyPasswordReset222(tx, p, now.Add(authSessionTTL)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset password")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit account recovery")
		return
	}
	a.resetLoginFailures(userID)
	a.audit("account_recovery_complete", userID, "target="+deviceID+" kind=no_code revoked_devices=")
	a.auditSecurity("account_recovery_complete", userID, deviceID, registrationKey(r), "no-code recovery completed with a new password")
	var account Account
	_ = a.store.DB().QueryRow(`SELECT id,username,created_at FROM users WHERE id=?`, userID).Scan(&account.ID, &account.Username, &account.CreatedAt)
	profile := profileForUserDB(a.store.DB(), userID, username)
	writeJSON(w, http.StatusOK, struct {
		AccountTransferResponse
		RecoveryCode string `json:"recovery_code"`
	}{AccountTransferResponse{
		Account: account,
		Device:  Device{ID: deviceID, Name: deviceName, Platform: platform, CreatedAt: p.NowText, LastSeenAt: p.NowText, Status: "active"},
		Profile: profile, Token: p.sessionToken, ExpiresAt: p.sessionExpires,
	}, p.code})
}
