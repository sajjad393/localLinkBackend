package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const accountTransferTTL = 10 * time.Minute

// AccountTransferStart is returned only to the authenticated source device.
// The secret is a high-entropy one-time bearer credential; it is not a password.
type AccountTransferStart struct {
	TransferID string `json:"transfer_id"`
	Payload    string `json:"payload"`
	ExpiresAt  string `json:"expires_at"`
}

type AccountTransferResponse struct {
	Account             Account `json:"account"`
	Device              Device  `json:"device"`
	Profile             Profile `json:"profile"`
	Token               string  `json:"token"`
	ExpiresAt           string  `json:"expires_at"`
	SourceDeviceID      string  `json:"source_device_id"`
	SourceDeviceRevoked bool    `json:"source_device_revoked"`
}

func (a *API) StartAccountTransfer(w http.ResponseWriter, r *http.Request) {
	deviceID, userID, username, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !a.allowSecurityAction("transfer-start", deviceID, maxSensitiveAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "transfer rate limit exceeded")
		return
	}

	// Keep only one active QR per source device. Starting a new one invalidates
	// the previous QR without deleting its audit history.
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	_, _ = a.store.DB().Exec(`UPDATE account_transfer_sessions SET consumed_at=? WHERE source_device_id=? AND consumed_at IS NULL`, nowText, deviceID)

	var serverID, secret string
	if err := a.store.DB().QueryRow(`SELECT server_id,identity_secret FROM server_identity WHERE singleton=1`).Scan(&serverID, new(string)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			secretBytes := make([]byte, 32)
			if _, err := rand.Read(secretBytes); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to initialize server identity")
				return
			}
			serverID = uuid.NewString()
			identitySecret := base64.RawURLEncoding.EncodeToString(secretBytes)
			if _, err := a.store.DB().Exec(`INSERT INTO server_identity(singleton,server_id,identity_secret,created_at) VALUES(1,?,?,?)`, serverID, identitySecret, nowText); err != nil {
				if err := a.store.DB().QueryRow(`SELECT server_id FROM server_identity WHERE singleton=1`).Scan(&serverID); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to initialize server identity")
					return
				}
			}
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read server identity")
			return
		}
	}

	var fingerprint string
	var rawIdentity string
	if err := a.store.DB().QueryRow(`SELECT identity_secret FROM server_identity WHERE singleton=1`).Scan(&rawIdentity); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read server identity")
		return
	}
	decoded, err := base64.RawURLEncoding.DecodeString(rawIdentity)
	if err != nil || len(decoded) != 32 {
		writeError(w, http.StatusInternalServerError, "stored server identity is invalid")
		return
	}
	// PairingInfo computes the same public fingerprint. Avoid exporting the identity secret.
	fingerprint = serverFingerprint(decoded)

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate transfer secret")
		return
	}
	secret = base64.RawURLEncoding.EncodeToString(secretBytes)
	transferID := uuid.NewString()
	expires := now.Add(accountTransferTTL)

	if _, err := a.store.DB().Exec(`INSERT INTO account_transfer_sessions(id,user_id,source_device_id,secret_hash,server_id,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`,
		transferID, userID, deviceID, tokenHash(secret), serverID, nowText, expires.Format(time.RFC3339Nano)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create account transfer")
		return
	}

	payload := map[string]string{
		"v":           "1",
		"server_id":   serverID,
		"fingerprint": fingerprint,
		"transfer_id": transferID,
		"secret":      secret,
		"expires_at":  expires.Format(time.RFC3339Nano),
		"username":    username,
	}
	raw, _ := json.Marshal(payload)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	qrPayload := "locallink://account-transfer/" + encoded

	a.audit("account_transfer_start", userID, "device="+deviceID)
	a.auditSecurity("account_transfer_start", userID, deviceID, registrationKey(r), "one-time transfer created")
	writeJSON(w, http.StatusCreated, AccountTransferStart{
		TransferID: transferID,
		Payload:    qrPayload,
		ExpiresAt:  expires.Format(time.RFC3339Nano),
	})
}

func (a *API) CancelAccountTransfer(w http.ResponseWriter, r *http.Request) {
	deviceID, userID, _, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in struct {
		TransferID string `json:"transfer_id"`
	}
	if err := decodeJSON(r, &in, 4<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	transferID := strings.TrimSpace(in.TransferID)
	if transferID == "" || len(transferID) > 128 || hasControlChars(transferID) {
		writeError(w, http.StatusBadRequest, "invalid transfer id")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := a.store.DB().Exec(`UPDATE account_transfer_sessions SET consumed_at=? WHERE id=? AND user_id=? AND source_device_id=? AND consumed_at IS NULL`, now, transferID, userID, deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel transfer")
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "transfer is not active")
		return
	}
	a.audit("account_transfer_cancel", userID, "device="+deviceID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) CompleteAccountTransfer(w http.ResponseWriter, r *http.Request) {
	if !a.allowSecurityAction("transfer-complete", registrationKey(r), maxSensitiveAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "transfer rate limit exceeded")
		return
	}
	var in struct {
		TransferID         string `json:"transfer_id"`
		Secret             string `json:"secret"`
		DeviceID           string `json:"device_id"`
		DeviceName         string `json:"device_name"`
		Platform           string `json:"platform"`
		IdentityPublicKey  string `json:"identity_public_key"`
		RevokeSourceDevice bool   `json:"revoke_source_device"`
	}
	if err := decodeJSON(r, &in, 12<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.TransferID = strings.TrimSpace(in.TransferID)
	in.Secret = strings.TrimSpace(in.Secret)
	in.DeviceID = strings.TrimSpace(in.DeviceID)
	in.DeviceName = strings.TrimSpace(in.DeviceName)
	in.Platform = strings.TrimSpace(in.Platform)
	in.IdentityPublicKey = strings.TrimSpace(in.IdentityPublicKey)
	if in.TransferID == "" || len(in.TransferID) > 128 || hasControlChars(in.TransferID) || in.Secret == "" || len(in.Secret) > 256 || in.DeviceID == "" || len(in.DeviceID) > 128 || hasControlChars(in.DeviceID) || in.DeviceName == "" || len(in.DeviceName) > maxDeviceName || hasControlChars(in.DeviceName) {
		writeError(w, http.StatusBadRequest, "invalid transfer or device data")
		return
	}
	if in.Platform == "" {
		in.Platform = "android"
	}
	if !validIdentityPublicKey(in.IdentityPublicKey) {
		writeError(w, http.StatusBadRequest, "invalid identity public key")
		return
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transfer transaction")
		return
	}
	defer tx.Rollback()

	var userID, sourceDeviceID, serverID string
	var expires string
	err = tx.QueryRow(`SELECT user_id,source_device_id,server_id,expires_at FROM account_transfer_sessions WHERE id=? AND consumed_at IS NULL`, in.TransferID).Scan(&userID, &sourceDeviceID, &serverID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusGone, "transfer is expired or already used")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read transfer")
		return
	}
	parsedExpiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || !now.Before(parsedExpiry) {
		_, _ = tx.Exec(`UPDATE account_transfer_sessions SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, nowText, in.TransferID)
		writeError(w, http.StatusGone, "transfer is expired")
		return
	}
	if !sameTokenHash(tx, `SELECT secret_hash FROM account_transfer_sessions WHERE id=?`, []any{in.TransferID}, in.Secret) {
		writeError(w, http.StatusUnauthorized, "invalid transfer credential")
		return
	}

	var account Account
	var accountStatus string
	if err := tx.QueryRow(`SELECT id,username,created_at,status FROM users WHERE id=?`, userID).Scan(&account.ID, &account.Username, &account.CreatedAt, &accountStatus); err != nil {
		writeError(w, http.StatusNotFound, "account no longer exists")
		return
	}
	if accountStatus != "active" {
		writeError(w, http.StatusForbidden, "account is not active")
		return
	}

	var sourceStatus string
	if err := tx.QueryRow(`SELECT COALESCE(status,'active') FROM devices WHERE id=? AND user_id=?`, sourceDeviceID, userID).Scan(&sourceStatus); err != nil || sourceStatus != "active" {
		writeError(w, http.StatusForbidden, "source device is no longer active")
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

	// Register a fresh device identity. The old phone's Android Keystore key is never copied.
	token, err := newToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate device session")
		return
	}
	sessionID := uuid.NewString()
	sessionExpires := now.Add(authSessionTTL)
	if _, err := tx.Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash,status,identity_public_key,user_id) VALUES(?,?,?,?,?,?, 'active', ?,?)`,
		in.DeviceID, in.DeviceName, in.Platform, nowText, nowText, tokenHash(token), in.IdentityPublicKey, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to register new device")
		return
	}
	if err := ensureIdentityKeyHistory(tx, in.DeviceID, in.IdentityPublicKey, 1, nowText); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize new device identity history")
		return
	}
	if _, err := tx.Exec(`INSERT INTO auth_sessions(id,user_id,device_id,token_hash,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?)`,
		sessionID, userID, in.DeviceID, tokenHash(token), nowText, nowText, sessionExpires.Format(time.RFC3339Nano)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create new device session")
		return
	}

	// Move device-owned group membership/ownership to the new device so replacing
	// a phone does not orphan groups. Historical messages remain associated with
	// the original device ID for audit/history purposes.
	if in.RevokeSourceDevice {
		if err := migrateOwnedGroups(tx, sourceDeviceID, in.DeviceID, userID, nowText); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to migrate group membership")
			return
		}
	}

	if _, err := tx.Exec(`UPDATE account_transfer_sessions SET consumed_at=?,target_device_id=? WHERE id=? AND consumed_at IS NULL`, nowText, in.DeviceID, in.TransferID); err != nil {
		writeError(w, http.StatusConflict, "transfer was already used")
		return
	}

	if in.RevokeSourceDevice {
		if _, err := tx.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, nowText, sourceDeviceID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revoke source sessions")
			return
		}
		if _, err := tx.Exec(`UPDATE devices SET status='revoked',revoked_at=?,token_hash=NULL WHERE id=? AND user_id=?`, nowText, sourceDeviceID, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revoke source device")
			return
		}
		if _, err := tx.Exec(`UPDATE device_identity_keys SET retired_at=? WHERE device_id=? AND retired_at IS NULL`, nowText, sourceDeviceID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to retire source device identity keys")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit account transfer")
		return
	}

	profile := profileForUserDB(a.store.DB(), userID, account.Username)
	if profile.UserID == "" {
		writeError(w, http.StatusInternalServerError, "failed to read migrated profile")
		return
	}
	target := Device{ID: in.DeviceID, Name: in.DeviceName, Platform: in.Platform, CreatedAt: nowText, LastSeenAt: nowText, Status: "active"}
	if in.RevokeSourceDevice {
		a.hub.CloseDevice(sourceDeviceID)
		d, _ := a.setPresence(sourceDeviceID, false, false, DevicePresence{})
		a.broadcastPresence(sourceDeviceID, d)
	}
	a.audit("account_transfer_complete", userID, "source="+sourceDeviceID+" target="+in.DeviceID+" revoke_source="+boolText(in.RevokeSourceDevice))
	a.auditSecurity("account_transfer_complete", userID, in.DeviceID, registrationKey(r), "transfer completed")

	writeJSON(w, http.StatusOK, AccountTransferResponse{
		Account:             account,
		Device:              target,
		Profile:             profile,
		Token:               token,
		ExpiresAt:           sessionExpires.Format(time.RFC3339Nano),
		SourceDeviceID:      sourceDeviceID,
		SourceDeviceRevoked: in.RevokeSourceDevice,
	})

}

func validIdentityPublicKey(value string) bool {
	if value == "" || len(value) > 128 || hasControlChars(value) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(raw) == 32
}

func sameTokenHash(tx *sql.Tx, query string, args []any, secret string) bool {
	var stored string
	if err := tx.QueryRow(query, args...).Scan(&stored); err != nil || stored == "" {
		return false
	}
	return secureTokenEqual(stored, tokenHash(secret))
}

func migrateOwnedGroups(tx *sql.Tx, sourceID, targetID, userID, nowText string) error {
	rows, err := tx.Query(`SELECT group_id,role,joined_at FROM group_members WHERE device_id=?`, sourceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type membership struct{ groupID, role, joinedAt string }
	var memberships []membership
	for rows.Next() {
		var m membership
		if err := rows.Scan(&m.groupID, &m.role, &m.joinedAt); err != nil {
			return err
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range memberships {
		if _, err := tx.Exec(`INSERT INTO group_members(group_id,device_id,role,joined_at) VALUES(?,?,?,?) ON CONFLICT(group_id,device_id) DO UPDATE SET role=excluded.role`, m.groupID, targetID, m.role, m.joinedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE groups SET owner_id=? WHERE owner_id=? AND EXISTS (SELECT 1 FROM devices d WHERE d.id=? AND d.user_id=?)`, targetID, sourceID, targetID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM group_members WHERE device_id=?`, sourceID); err != nil {
		return err
	}
	_ = nowText
	return nil
}

func profileForUserDB(db interface{ QueryRow(string, ...any) *sql.Row }, userID, username string) Profile {
	var displayName, avatarPath, avatarUpdatedAt, updatedAt string
	var avatarSize int64
	err := db.QueryRow(`SELECT display_name,COALESCE(avatar_path,''),COALESCE(avatar_updated_at,''),updated_at,COALESCE(avatar_size,0) FROM profiles WHERE user_id=?`, userID).Scan(&displayName, &avatarPath, &avatarUpdatedAt, &updatedAt, &avatarSize)
	if err != nil {
		return Profile{}
	}
	avatarURL := ""
	if avatarPath != "" && avatarSize > 0 {
		avatarURL = "/api/v1/profile/avatar"
	}
	return Profile{UserID: userID, Username: username, DisplayName: displayName, AvatarURL: avatarURL, AvatarUpdatedAt: avatarUpdatedAt, UpdatedAt: updatedAt}
}

func serverFingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
