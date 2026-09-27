package httpapi

import (
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crypto/sha256"
)

type IdentityKeyHistoryRecord struct {
	DeviceID    string `json:"device_id"`
	KeyVersion  int    `json:"key_version"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
	CreatedAt   string `json:"created_at"`
	RetiredAt   string `json:"retired_at,omitempty"`
	Active      bool   `json:"active"`
}

type RotateIdentityKeyResponse struct {
	DeviceID               string `json:"device_id"`
	KeyVersion             int    `json:"key_version"`
	PublicKey              string `json:"public_key"`
	Fingerprint            string `json:"fingerprint"`
	PreviousKeyVersion     int    `json:"previous_key_version"`
	PreviousFingerprint    string `json:"previous_fingerprint,omitempty"`
	MigrationGeneration    string `json:"migration_generation"`
	LegacyHistoryPreserved bool   `json:"legacy_history_preserved"`
}

func identityKeyFingerprint(publicKey string) string {
	raw, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func ensureIdentityKeyHistory(tx *sql.Tx, deviceID, publicKey string, version int, createdAt string) error {
	if deviceID == "" || publicKey == "" || version < 1 {
		return errors.New("invalid identity key history record")
	}
	_, err := tx.Exec(`INSERT OR IGNORE INTO device_identity_keys(device_id,key_version,public_key,created_at) VALUES(?,?,?,?)`, deviceID, version, publicKey, createdAt)
	return err
}

func (a *API) RotateIdentityKey(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !a.allowSecurityAction("identity-key-rotate", deviceID, maxSensitiveAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "identity key rotation rate limit exceeded")
		return
	}
	var in struct {
		PublicKey string `json:"public_key"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	publicKey := strings.TrimSpace(in.PublicKey)
	if !validIdentityPublicKey(publicKey) {
		writeError(w, http.StatusBadRequest, "public_key must be a base64url-encoded X25519 public key")
		return
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start key rotation")
		return
	}
	defer tx.Rollback()

	var currentKey string
	var currentVersion int
	var status string
	if err := tx.QueryRow(`SELECT COALESCE(identity_public_key,''),COALESCE(identity_key_version,1),COALESCE(status,'active') FROM devices WHERE id=?`, deviceID).Scan(&currentKey, &currentVersion, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "device not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load device identity")
		}
		return
	}
	if status != "active" {
		writeError(w, http.StatusForbidden, "device is not active")
		return
	}
	if currentKey == "" {
		writeError(w, http.StatusConflict, "device has no existing identity key; initialize it first")
		return
	}
	if currentKey == publicKey {
		writeError(w, http.StatusConflict, "new identity key must differ from the current key")
		return
	}

	if err := ensureIdentityKeyHistory(tx, deviceID, currentKey, currentVersion, nowText); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preserve previous identity key")
		return
	}

	nextVersion := currentVersion + 1
	if nextVersion <= currentVersion {
		writeError(w, http.StatusInternalServerError, "identity key version overflow")
		return
	}
	if _, err := tx.Exec(`UPDATE device_identity_keys SET retired_at=? WHERE device_id=? AND key_version=? AND retired_at IS NULL`, nowText, deviceID, currentVersion); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retire previous identity key")
		return
	}
	if _, err := tx.Exec(`INSERT INTO device_identity_keys(device_id,key_version,public_key,created_at) VALUES(?,?,?,?)`, deviceID, nextVersion, publicKey, nowText); err != nil {
		writeError(w, http.StatusConflict, "identity key version already exists")
		return
	}
	if _, err := tx.Exec(`UPDATE devices SET identity_public_key=?,identity_key_version=?,last_seen_at=? WHERE id=?`, publicKey, nextVersion, nowText, deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate new identity key")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit key rotation")
		return
	}

	previousFingerprint := identityKeyFingerprint(currentKey)
	fingerprint := identityKeyFingerprint(publicKey)
	generation := nowText + "#" + deviceID + "#" + itoa(nextVersion)
	a.audit("rotate_identity_key", deviceID, "version="+itoa(nextVersion))
	a.auditSecurity("rotate_identity_key", "", deviceID, registrationKey(r), "device identity key rotated")
	writeJSON(w, http.StatusOK, RotateIdentityKeyResponse{
		DeviceID:               deviceID,
		KeyVersion:             nextVersion,
		PublicKey:              publicKey,
		Fingerprint:            fingerprint,
		PreviousKeyVersion:     currentVersion,
		PreviousFingerprint:    previousFingerprint,
		MigrationGeneration:    generation,
		LegacyHistoryPreserved: true,
	})
}

func (a *API) IdentityKeyHistory(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	rows, err := a.store.DB().Query(`SELECT device_id,key_version,public_key,created_at,COALESCE(retired_at,'') FROM device_identity_keys WHERE device_id=? ORDER BY key_version DESC`, deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read identity key history")
		return
	}
	defer rows.Close()
	out := make([]IdentityKeyHistoryRecord, 0)
	for rows.Next() {
		var deviceIDValue, publicKey, createdAt, retiredAt string
		var version int
		if err := rows.Scan(&deviceIDValue, &version, &publicKey, &createdAt, &retiredAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read identity key history")
			return
		}
		out = append(out, IdentityKeyHistoryRecord{
			DeviceID:    deviceIDValue,
			KeyVersion:  version,
			PublicKey:   publicKey,
			Fingerprint: identityKeyFingerprint(publicKey),
			CreatedAt:   createdAt,
			RetiredAt:   retiredAt,
			Active:      retiredAt == "",
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to iterate identity key history")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

// AllIdentityKeyHistory returns public identity-key history for active devices
// belonging to the same account. Public keys are safe to distribute, while
// the private keys remain exclusively on their originating devices.
func (a *API) AllIdentityKeyHistory(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var userID string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(user_id,'') FROM devices WHERE id=?`, deviceID).Scan(&userID); err != nil || userID == "" {
		writeError(w, http.StatusForbidden, "device is not attached to an account")
		return
	}
	rows, err := a.store.DB().Query(`
SELECT dik.device_id,dik.key_version,dik.public_key,dik.created_at,COALESCE(dik.retired_at,'')
FROM device_identity_keys dik
JOIN devices d ON d.id=dik.device_id
WHERE d.user_id=?
ORDER BY dik.device_id,dik.key_version DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read account identity-key history")
		return
	}
	defer rows.Close()
	out := make([]IdentityKeyHistoryRecord, 0)
	for rows.Next() {
		var deviceValue, publicKey, createdAt, retiredAt string
		var version int
		if err := rows.Scan(&deviceValue, &version, &publicKey, &createdAt, &retiredAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read account identity-key history")
			return
		}
		out = append(out, IdentityKeyHistoryRecord{
			DeviceID: deviceValue, KeyVersion: version, PublicKey: publicKey,
			Fingerprint: identityKeyFingerprint(publicKey), CreatedAt: createdAt,
			RetiredAt: retiredAt, Active: retiredAt == "",
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to iterate account identity-key history")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
