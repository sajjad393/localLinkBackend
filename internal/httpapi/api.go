package httpapi

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"locallink/backend/internal/store"
	"locallink/backend/internal/ws"
)

const (
	maxDeviceName          = 64
	maxMessageLen          = 16 * 1024
	maxEncryptedMessageLen = 48 * 1024
	directMessageEnvelope  = "e2e:v2:"
)

type API struct {
	store            *store.Store
	hub              *ws.Hub
	rateMu           sync.Mutex
	rates            map[string]messageRate
	regRateMu        sync.Mutex
	regRates         map[string]messageRate
	securityMu       sync.Mutex
	securityRates    map[string]messageRate
	fileRoot         string
	maxFileSize      int64
	fileUploadGate   chan struct{}
	fileDownloadGate chan struct{}
	presenceMu       sync.RWMutex
	presence         map[string]DevicePresence
	runtimeMu        sync.RWMutex
	runtimeProvider  func() map[string]any
}

type messageRate struct {
	started time.Time
	count   int
}

const messagesPerMinute = 120

func New(s *store.Store) *API {
	api, err := NewWithError(s)
	if err != nil {
		panic(err)
	}
	return api
}

func NewWithError(s *store.Store) (*API, error) {
	root := strings.TrimSpace(os.Getenv("LOCALLINK_FILES_DIR"))
	if root == "" {
		root = "data/files"
	}
	maxFileSize := defaultMaxFileSize
	if raw := strings.TrimSpace(os.Getenv("LOCALLINK_MAX_FILE_MB")); raw != "" {
		if mb, err := strconv.ParseInt(raw, 10, 64); err == nil && mb >= 1 && mb <= 2048 {
			maxFileSize = mb * 1024 * 1024
		}
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create attachment storage root: %w", err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		if err != nil {
			return nil, fmt.Errorf("stat attachment storage root: %w", err)
		}
		return nil, fmt.Errorf("attachment storage root is not a directory")
	}
	api := &API{
		store:            s,
		hub:              ws.NewHub(),
		rates:            make(map[string]messageRate),
		regRates:         make(map[string]messageRate),
		securityRates:    make(map[string]messageRate),
		fileRoot:         root,
		maxFileSize:      maxFileSize,
		fileUploadGate:   make(chan struct{}, 8),
		fileDownloadGate: make(chan struct{}, 16),
		presence:         make(map[string]DevicePresence),
	}
	envName := strings.ToLower(strings.TrimSpace(getenv("LOCALLINK_ENV", "production")))
	if envName != "test" && envName != "development" {
		if err := api.ensureBootstrapAdmin(); err != nil {
			return nil, err
		}
	}
	return api, nil
}

type Device struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Platform             string `json:"platform"`
	CreatedAt            string `json:"created_at"`
	LastSeenAt           string `json:"last_seen_at"`
	Status               string `json:"status"`
	RevokedAt            string `json:"revoked_at,omitempty"`
	IdentityFingerprint  string `json:"identity_fingerprint,omitempty"`
	NetworkStatus        string `json:"network_status"`
	ServerConnected      bool   `json:"server_connected"`
	WiFiDirectConnected  bool   `json:"wifi_direct_connected"`
	LanAvailable         bool   `json:"lan_available,omitempty"`
	MeshAvailable        bool   `json:"mesh_available,omitempty"`
	LastCapabilityUpdate string `json:"last_capability_update,omitempty"`
	SigningPublicKey     string `json:"signing_public_key,omitempty"`
	WiFiControlCapable   bool   `json:"wifi_control_capable,omitempty"`
	ManagedDevice        bool   `json:"managed_device,omitempty"`
	WiFiEnabled          bool   `json:"wifi_enabled,omitempty"`
	StatusUpdatedAt      string `json:"status_updated_at"`
}

type DevicePresence struct {
	ServerConnected      bool
	WiFiDirectConnected  bool
	LanAvailable         bool
	MeshAvailable        bool
	LastCapabilityUpdate string
	SigningPublicKey     string
	WiFiControlCapable   bool
	ManagedDevice        bool
	WiFiEnabled          bool
	UpdatedAt            string
}

type PeerKey struct {
	PeerID string `json:"peer_id"`
	Key    string `json:"key"`
}

const presenceTTL = 35 * time.Second

type Message struct {
	ID          string       `json:"id"`
	SenderID    string       `json:"sender_id"`
	RecipientID string       `json:"recipient_id"`
	Body        string       `json:"body"`
	CreatedAt   string       `json:"created_at"`
	Status      string       `json:"status"`
	DeliveredAt string       `json:"delivered_at,omitempty"`
	ServerSeq   int64        `json:"server_seq"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type PairingInfo struct {
	Service         string `json:"service"`
	Version         string `json:"version"`
	ServerID        string `json:"server_id"`
	Fingerprint     string `json:"fingerprint"`
	Name            string `json:"name"`
	PairingRequired bool   `json:"pairing_required"`
}

func (a *API) PairingInfo(w http.ResponseWriter, r *http.Request) {
	var serverID, secret, createdAt string
	err := a.store.DB().QueryRow(`SELECT server_id,identity_secret,created_at FROM server_identity WHERE singleton=1`).Scan(&serverID, &secret, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to initialize server identity")
			return
		}
		serverID = uuid.NewString()
		secret = base64.RawURLEncoding.EncodeToString(secretBytes)
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := a.store.DB().Exec(`INSERT INTO server_identity(singleton,server_id,identity_secret,created_at) VALUES(1,?,?,?)`, serverID, secret, createdAt); err != nil {
			// Another request may have initialized it concurrently; read again.
			if err2 := a.store.DB().QueryRow(`SELECT server_id,identity_secret,created_at FROM server_identity WHERE singleton=1`).Scan(&serverID, &secret, &createdAt); err2 != nil {
				writeError(w, http.StatusInternalServerError, "failed to initialize server identity")
				return
			}
		}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read server identity")
		return
	}
	rawSecret, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(rawSecret) != 32 {
		writeError(w, http.StatusInternalServerError, "stored server identity is invalid")
		return
	}
	sum := sha256.Sum256(rawSecret)
	fingerprint := strings.ToUpper(hex.EncodeToString(sum[:]))
	writeJSON(w, http.StatusOK, PairingInfo{
		Service:         "locallink",
		Version:         "1",
		ServerID:        serverID,
		Fingerprint:     fingerprint,
		Name:            getenv("LOCALLINK_NAME", "LocalLink Server"),
		PairingRequired: strings.TrimSpace(os.Getenv("LOCALLINK_PAIRING_CODE")) != "",
	})
}

func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "locallink",
		"time":    time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (a *API) Status(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/health" {
		if _, ok := a.authorizeDevice(r); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}
	d, _ := a.store.Count("devices")
	m, _ := a.store.Count("messages")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"service":  "locallink",
		"version":  "phase-l",
		"devices":  d,
		"messages": m,
	})
}

func validNetworkMessageBody(body string) bool {
	return strings.HasPrefix(body, directMessageEnvelope) && len(body) <= maxEncryptedMessageLen
}

func validateDirectE2EEnvelope(body, senderID, recipientID, messageID string) error {
	if !validNetworkMessageBody(body) {
		return errors.New("direct messages must use e2e:v2")
	}
	raw := strings.TrimPrefix(body, directMessageEnvelope)
	var envelope struct {
		Version     int    `json:"version"`
		Type        string `json:"type"`
		SenderID    string `json:"sender_id"`
		RecipientID string `json:"recipient_id"`
		MessageID   string `json:"message_id"`
		CreatedAt   string `json:"created_at"`
		Nonce       string `json:"nonce"`
		Ciphertext  string `json:"ciphertext"`
		Mac         string `json:"mac"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&envelope); err != nil {
		return errors.New("invalid e2e:v2 envelope")
	}
	if envelope.Version != 2 || envelope.Type != "direct_message" ||
		envelope.SenderID != senderID || envelope.RecipientID != recipientID ||
		envelope.MessageID != messageID || envelope.CreatedAt == "" ||
		envelope.Nonce == "" || envelope.Ciphertext == "" || envelope.Mac == "" {
		return errors.New("e2e:v2 envelope identity binding is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt); err != nil {
		if _, err := time.Parse(time.RFC3339, envelope.CreatedAt); err != nil {
			return errors.New("e2e:v2 envelope timestamp is invalid")
		}
	}
	nonce, err := base64.RawURLEncoding.DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != 12 {
		return errors.New("e2e:v2 nonce is invalid")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	if err != nil || len(ciphertext) == 0 {
		return errors.New("e2e:v2 ciphertext is invalid")
	}
	mac, err := base64.RawURLEncoding.DecodeString(envelope.Mac)
	if err != nil || len(mac) != 16 {
		return errors.New("e2e:v2 mac is invalid")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("e2e:v2 envelope must contain one JSON object")
	}
	return nil
}

func (a *API) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	if !a.allowRegistration(registrationKey(r)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "device registration rate limit exceeded")
		return
	}
	var in struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Platform    string `json:"platform"`
		PairingCode string `json:"pairing_code"`
		Token       string `json:"token"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	in.Name = strings.TrimSpace(in.Name)
	in.ID = strings.TrimSpace(in.ID)
	in.Platform = strings.TrimSpace(in.Platform)
	in.PairingCode = strings.TrimSpace(in.PairingCode)
	in.Token = strings.TrimSpace(in.Token)
	if in.Name == "" || len(in.Name) > maxDeviceName || hasControlChars(in.Name) {
		writeError(w, http.StatusBadRequest, "name must be 1-64 characters")
		return
	}
	if in.Platform == "" {
		in.Platform = "android"
	}
	if len(in.ID) > 128 || hasControlChars(in.ID) {
		writeError(w, http.StatusBadRequest, "invalid device id")
		return
	}

	id := in.ID
	if id != "" {
		var existing Device
		var hash, status string
		err := a.store.DB().QueryRow(`
SELECT id,name,platform,created_at,last_seen_at,COALESCE(token_hash,''),COALESCE(status,'active')
FROM devices WHERE id=?`, id).Scan(&existing.ID, &existing.Name, &existing.Platform, &existing.CreatedAt, &existing.LastSeenAt, &hash, &status)
		if err == nil {
			if status != "active" {
				writeError(w, http.StatusForbidden, "device is revoked and must be re-enrolled with a new device identity")
				return
			}
			// Existing devices prove ownership through their bearer token. The
			// deployment pairing code is for first-time enrollment only.
			if in.Token == "" || !secureTokenEqual(hash, tokenHash(in.Token)) {
				writeError(w, http.StatusUnauthorized, "invalid device token")
				return
			}
			now := time.Now().UTC().Format(time.RFC3339Nano)
			if _, err := a.store.DB().Exec(`UPDATE devices SET name=?,platform=?,last_seen_at=? WHERE id=?`, in.Name, in.Platform, now, id); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update device")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"device":   Device{ID: id, Name: in.Name, Platform: in.Platform, CreatedAt: existing.CreatedAt, LastSeenAt: now},
				"token":    in.Token,
				"existing": true,
			})
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to lookup device")
			return
		}
	}

	requiredPairing := strings.TrimSpace(os.Getenv("LOCALLINK_PAIRING_CODE"))
	if requiredPairing != "" && in.PairingCode != requiredPairing {
		writeError(w, http.StatusForbidden, "invalid pairing code")
		return
	}

	if id == "" {
		id = uuid.NewString()
	}
	token, err := newToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate device credentials")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = a.store.DB().Exec(`
INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash)
VALUES(?,?,?,?,?,?)`, id, in.Name, in.Platform, now, now, tokenHash(token))
	if err != nil {
		writeError(w, http.StatusConflict, "device registration conflict")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"device":   Device{ID: id, Name: in.Name, Platform: in.Platform, CreatedAt: now, LastSeenAt: now},
		"token":    token,
		"existing": false,
	})
}

func (a *API) PutIdentityKey(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in struct {
		PublicKey string `json:"public_key"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	value := strings.TrimSpace(in.PublicKey)
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		writeError(w, http.StatusBadRequest, "public_key must be a base64url-encoded X25519 public key")
		return
	}
	var existing string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(identity_public_key,'') FROM devices WHERE id=?`, deviceID).Scan(&existing); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load identity key")
		return
	}
	if existing != "" && existing != value {
		writeError(w, http.StatusConflict, "identity key is already bound; rotate it through the device-key rotation flow")
		return
	}
	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	if existing == "" {
		if _, err := a.store.DB().Exec(`UPDATE devices SET identity_public_key=?,identity_key_version=1,last_seen_at=? WHERE id=?`, value, nowText, deviceID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to store identity key")
			return
		}
		if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO device_identity_keys(device_id,key_version,public_key,created_at) VALUES(?,?,?,?)`, deviceID, 1, value, nowText); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record identity key history")
			return
		}
	} else if _, err := a.store.DB().Exec(`UPDATE devices SET identity_public_key=? WHERE id=?`, value, deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store identity key")
		return
	}
	a.audit("update_identity_key", deviceID, "x25519")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) IdentityKeys(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var userID string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(user_id,'') FROM devices WHERE id=?`, deviceID).Scan(&userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect device account")
		return
	}
	var rows *sql.Rows
	var err error
	if userID == "" {
		// Preserve the pre-account device protocol for legacy deployments.
		rows, err = a.store.DB().Query(`SELECT id,COALESCE(identity_public_key,''),COALESCE(identity_key_version,1) FROM devices WHERE id != ? AND COALESCE(status,'active')='active' ORDER BY id`, deviceID)
	} else {
		rows, err = a.store.DB().Query(`SELECT id,COALESCE(identity_public_key,''),COALESCE(identity_key_version,1) FROM devices WHERE id != ? AND user_id=? AND COALESCE(status,'active')='active' ORDER BY id`, deviceID, userID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identity keys")
		return
	}
	defer rows.Close()
	type keyRecord struct {
		PeerID     string `json:"peer_id"`
		PublicKey  string `json:"public_key"`
		KeyVersion int    `json:"key_version"`
	}
	out := make([]keyRecord, 0)
	for rows.Next() {
		var id, key string
		var keyVersion int
		if err := rows.Scan(&id, &key, &keyVersion); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read identity key")
			return
		}
		if key == "" {
			continue
		}
		decoded, err := base64.RawURLEncoding.DecodeString(key)
		if err != nil || len(decoded) != 32 {
			continue
		}
		out = append(out, keyRecord{PeerID: id, PublicKey: key, KeyVersion: keyVersion})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read identity keys")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func deviceIdentityFingerprint(publicKey string) string {
	value := strings.TrimSpace(publicKey)
	if value == "" {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return ""
	}
	sum := sha256.Sum256(decoded)
	return hex.EncodeToString(sum[:])
}

func (a *API) deviceFromRow(id, name, platform, createdAt, lastSeenAt, status, revokedAt, publicKey string) Device {
	return Device{
		ID: id, Name: name, Platform: platform, CreatedAt: createdAt, LastSeenAt: lastSeenAt,
		Status: status, RevokedAt: revokedAt, IdentityFingerprint: deviceIdentityFingerprint(publicKey),
	}
}

func (a *API) ListAccountDevices(w http.ResponseWriter, r *http.Request) {
	currentID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var userID string
	if err := a.store.DB().QueryRow(`SELECT user_id FROM devices WHERE id=?`, currentID).Scan(&userID); err != nil || userID == "" {
		writeError(w, http.StatusNotFound, "account is not linked to this device")
		return
	}
	params, err := parsePageParams(r, "account-devices:"+userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{userID}
	where := `user_id=?`
	if params.Cursor != nil {
		rank := cursorRank(params.Cursor.ID, currentID)
		where += ` AND ((CASE WHEN id=? THEN 0 ELSE 1 END)>? OR ((CASE WHEN id=? THEN 0 ELSE 1 END)=? AND (name COLLATE NOCASE > ? OR (name COLLATE NOCASE=? AND id>?))))`
		args = append(args, currentID, rank, currentID, rank, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT id,name,platform,created_at,last_seen_at,COALESCE(status,'active'),COALESCE(revoked_at,''),COALESCE(identity_public_key,'') FROM devices WHERE `+where+` ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END,name COLLATE NOCASE,id LIMIT ?`, append(args[:len(args)-1], currentID, args[len(args)-1])...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list trusted devices")
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0, params.Limit)
	for rows.Next() {
		var id, name, platform, createdAt, lastSeenAt, status, revokedAt, publicKey string
		if err := rows.Scan(&id, &name, &platform, &createdAt, &lastSeenAt, &status, &revokedAt, &publicKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read trusted device")
			return
		}
		d := a.enrichDevicePresence(a.deviceFromRow(id, name, platform, createdAt, lastSeenAt, status, revokedAt, publicKey))
		out = append(out, map[string]any{
			"id": d.ID, "name": d.Name, "platform": d.Platform, "created_at": d.CreatedAt, "last_seen_at": d.LastSeenAt,
			"status": d.Status, "revoked_at": d.RevokedAt, "identity_fingerprint": d.IdentityFingerprint,
			"network_status": d.NetworkStatus, "server_connected": d.ServerConnected,
			"wifi_direct_connected": d.WiFiDirectConnected, "status_updated_at": d.StatusUpdatedAt, "current": d.ID == currentID,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read trusted devices")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Devices []map[string]any `json:"devices"`
		PageInfo
	}{Devices: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("account-devices:"+userID, last["name"].(string), last["id"].(string), false)
	}
	writeJSON(w, http.StatusOK, resp)
}

func cursorRank(id, currentID string) int {
	if id == currentID {
		return 0
	}
	return 1
}

func (a *API) RevokeAccountDevice(w http.ResponseWriter, r *http.Request) {
	currentID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in struct {
		DeviceID string `json:"device_id"`
	}
	_ = decodeJSON(r, &in, 4<<10)
	id := strings.TrimSpace(in.DeviceID)
	if id == "" {
		id = strings.TrimSpace(r.URL.Query().Get("device_id"))
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "device_id required")
		return
	}
	if id == currentID {
		writeError(w, http.StatusBadRequest, "current device cannot be revoked here; use logout")
		return
	}
	var userID, status string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(user_id,''),COALESCE(status,'active') FROM devices WHERE id=?`, id).Scan(&userID, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to inspect device")
		return
	}
	var currentUser string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(user_id,'') FROM devices WHERE id=?`, currentID).Scan(&currentUser); err != nil || currentUser == "" || currentUser != userID {
		writeError(w, http.StatusForbidden, "device does not belong to this account")
		return
	}
	if status == "revoked" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "already_revoked": true})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke device")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE devices SET status='revoked',revoked_at=? WHERE id=? AND user_id=?`, now, id, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke device")
		return
	}
	if _, err = tx.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, now, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke device sessions")
		return
	}
	if _, err = tx.Exec(`UPDATE device_identity_keys SET retired_at=? WHERE device_id=? AND retired_at IS NULL`, now, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retire device identity keys")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit device revocation")
		return
	}
	a.hub.CloseDevice(id)
	a.presenceMu.Lock()
	delete(a.presence, id)
	a.presenceMu.Unlock()
	a.audit("revoke_account_device", id, "account="+userID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": true, "device_id": id})
}

func (a *API) ListDevices(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	_ = deviceID
	params, err := parsePageParams(r, "devices")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{}
	where := `COALESCE(status,'active')='active'`
	if params.Cursor != nil {
		where += ` AND (name COLLATE NOCASE > ? OR (name COLLATE NOCASE=? AND id>?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT id,name,platform,created_at,last_seen_at,COALESCE(status,'active'),COALESCE(revoked_at,''),COALESCE(identity_public_key,'') FROM devices WHERE `+where+` ORDER BY name COLLATE NOCASE,id LIMIT ?`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list devices")
		return
	}
	defer rows.Close()
	out := make([]Device, 0, params.Limit)
	for rows.Next() {
		var id, name, platform, createdAt, lastSeenAt, status, revokedAt, publicKey string
		if err := rows.Scan(&id, &name, &platform, &createdAt, &lastSeenAt, &status, &revokedAt, &publicKey); err != nil {
			writeError(w, 500, "failed to read device")
			return
		}
		d := a.enrichDevicePresence(a.deviceFromRow(id, name, platform, createdAt, lastSeenAt, status, revokedAt, publicKey))
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read devices")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Devices []Device `json:"devices"`
		PageInfo
	}{Devices: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("devices", last.Name, last.ID, false)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) enrichDevicePresence(d Device) Device {
	a.presenceMu.RLock()
	p, ok := a.presence[d.ID]
	a.presenceMu.RUnlock()
	if !ok {
		d.NetworkStatus = "unknown"
		return d
	}
	d.ServerConnected = p.ServerConnected
	d.WiFiDirectConnected = p.WiFiDirectConnected
	d.LanAvailable = p.LanAvailable
	d.MeshAvailable = p.MeshAvailable
	d.LastCapabilityUpdate = p.LastCapabilityUpdate
	d.SigningPublicKey = p.SigningPublicKey
	d.WiFiControlCapable = p.WiFiControlCapable
	d.ManagedDevice = p.ManagedDevice
	d.WiFiEnabled = p.WiFiEnabled
	d.StatusUpdatedAt = p.UpdatedAt
	if presenceStale(p) {
		d.NetworkStatus = "unknown"
		d.ServerConnected = false
		d.WiFiDirectConnected = false
		d.WiFiControlCapable = false
		d.ManagedDevice = false
		d.WiFiEnabled = false
	} else {
		d.NetworkStatus = presenceStatus(p)
	}
	return d
}

func presenceStale(p DevicePresence) bool {
	when, err := time.Parse(time.RFC3339Nano, p.UpdatedAt)
	return err != nil || time.Since(when) > presenceTTL
}

func presenceStatus(p DevicePresence) string {
	if p.WiFiDirectConnected {
		return "orange"
	}
	if p.ServerConnected {
		return "yellow"
	}
	return "red"
}

func (a *API) setPresence(deviceID string, serverConnected, wifiDirectConnected bool, extras ...DevicePresence) (Device, bool) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	next := DevicePresence{ServerConnected: serverConnected, WiFiDirectConnected: wifiDirectConnected, UpdatedAt: now}
	if len(extras) > 0 {
		next = extras[0]
		next.ServerConnected = serverConnected
		next.WiFiDirectConnected = wifiDirectConnected
		next.UpdatedAt = now
	}
	a.presenceMu.Lock()
	changed := true
	a.presence[deviceID] = next
	a.presenceMu.Unlock()
	_, _ = a.store.DB().Exec(`UPDATE devices SET last_seen_at=? WHERE id=?`, now, deviceID)

	var d Device
	_ = a.store.DB().QueryRow(`SELECT id,name,platform,created_at,last_seen_at FROM devices WHERE id=?`, deviceID).Scan(&d.ID, &d.Name, &d.Platform, &d.CreatedAt, &d.LastSeenAt)
	d = a.enrichDevicePresence(d)
	return d, changed
}

func (a *API) broadcastPresence(deviceID string, d Device) {
	payload := mustJSON(WSMessage{
		Type: "presence_update", DeviceID: d.ID, NetworkStatus: d.NetworkStatus,
		ServerConnected:     d.ServerConnected,
		WiFiDirectConnected: d.WiFiDirectConnected,
		LanAvailable:        d.LanAvailable, MeshAvailable: d.MeshAvailable,
		SigningPublicKey: d.SigningPublicKey, WiFiControlCapable: d.WiFiControlCapable, ManagedDevice: d.ManagedDevice, WiFiEnabled: d.WiFiEnabled, StatusUpdatedAt: d.StatusUpdatedAt, LastSeenAt: d.LastSeenAt,
	})
	for _, id := range a.hub.DeviceIDs() {
		if id == deviceID {
			continue
		}
		a.hub.SendTo(id, payload)
	}
}

func (a *API) PeerKeys(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "legacy shared peer keys are disabled; use device identity keys")
}

func (a *API) PeerKey(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "legacy shared peer keys are disabled; use device identity keys")
}

func (a *API) CreateMessage(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in struct {
		ID            string   `json:"id"`
		SenderID      string   `json:"sender_id"`
		RecipientID   string   `json:"recipient_id"`
		Body          string   `json:"body"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if err := decodeJSON(r, &in, maxEncryptedMessageLen+8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.ID = strings.TrimSpace(in.ID)
	in.SenderID = strings.TrimSpace(in.SenderID)
	in.RecipientID = strings.TrimSpace(in.RecipientID)
	if in.ID == "" || len(in.ID) > 128 || hasControlChars(in.ID) {
		writeError(w, http.StatusBadRequest, "message id is required")
		return
	}
	if in.RecipientID == "" || len(in.RecipientID) > 128 || hasControlChars(in.RecipientID) {
		writeError(w, http.StatusBadRequest, "invalid recipient id")
		return
	}
	if err := validateDirectE2EEnvelope(in.Body, deviceID, in.RecipientID, in.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.SenderID != "" && in.SenderID != deviceID {
		writeError(w, http.StatusForbidden, "sender_id does not match device")
		return
	}
	if in.RecipientID == deviceID {
		writeError(w, http.StatusBadRequest, "recipient must be another device")
		return
	}
	if in.Body == "" && len(in.AttachmentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "body or attachment is required")
		return
	}
	if len(in.AttachmentIDs) > maxAttachmentsPerMessage {
		writeError(w, http.StatusBadRequest, "too many attachments")
		return
	}
	if !a.deviceExists(in.RecipientID) {
		writeError(w, http.StatusNotFound, "recipient device not found")
		return
	}
	if !a.allowMessage(deviceID) {
		writeError(w, http.StatusTooManyRequests, "message rate limit exceeded")
		return
	}

	m, existing, err := a.insertMessage(deviceID, in.RecipientID, in.Body, in.ID, in.AttachmentIDs)
	if err != nil {
		if strings.Contains(err.Error(), "attachment") || strings.Contains(err.Error(), "different message") || strings.Contains(err.Error(), "too many") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to store message")
		return
	}
	if !existing {
		a.hub.SendTo(m.RecipientID, mustJSON(WSMessage{Type: "message", ID: m.ID, SenderID: m.SenderID, RecipientID: m.RecipientID, Body: m.Body, CreatedAt: m.CreatedAt, Status: m.Status, ServerSeq: m.ServerSeq, Attachments: m.Attachments}))
	}
	status := http.StatusCreated
	if existing {
		status = http.StatusOK
	}
	writeJSON(w, status, Message{
		ID: m.ID, SenderID: m.SenderID, RecipientID: m.RecipientID, Body: m.Body,
		CreatedAt: m.CreatedAt, Status: m.Status, DeliveredAt: m.DeliveredAt, ServerSeq: m.ServerSeq, Attachments: m.Attachments,
	})
}

func (a *API) ListMessages(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aID := strings.TrimSpace(r.URL.Query().Get("a"))
	bID := strings.TrimSpace(r.URL.Query().Get("b"))
	if aID == "" || bID == "" {
		writeError(w, http.StatusBadRequest, "a and b are required")
		return
	}
	if deviceID != aID && deviceID != bID {
		writeError(w, http.StatusForbidden, "device is not part of this conversation")
		return
	}
	params, err := parsePageParams(r, "messages:"+aID+":"+bID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{aID, bID, bID, aID}
	where := `((sender_id=? AND recipient_id=?) OR (sender_id=? AND recipient_id=?))`
	if params.Cursor != nil {
		where += ` AND (created_at > ? OR (created_at=? AND id>?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`
SELECT id,sender_id,recipient_id,body,created_at,status,COALESCE(delivered_at,''),COALESCE(server_seq,0)
FROM messages
WHERE `+where+`
ORDER BY created_at ASC,id ASC
LIMIT ?`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	defer rows.Close()
	out := make([]Message, 0, params.Limit)
	ids := make([]string, 0, params.Limit)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &m.CreatedAt, &m.Status, &m.DeliveredAt, &m.ServerSeq); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read message")
			return
		}
		out = append(out, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read messages")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
		ids = ids[:params.Limit]
	}
	attachmentMap, err := a.attachmentsForMessages(ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read attachments")
		return
	}
	for i := range out {
		out[i].Attachments = attachmentMap[out[i].ID]
	}
	resp := struct {
		Messages []Message `json:"messages"`
		PageInfo
	}{Messages: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("messages:"+aID+":"+bID, last.CreatedAt, last.ID, false)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) deviceExists(id string) bool {
	var exists int
	return a.store.DB().QueryRow("SELECT 1 FROM devices WHERE id=? AND COALESCE(status,'active')='active' LIMIT 1", id).Scan(&exists) == nil
}

func (a *API) userExists(id string) bool {
	var exists int
	return a.store.DB().QueryRow("SELECT 1 FROM users WHERE id=? LIMIT 1", id).Scan(&exists) == nil
}

func (a *API) authorizeDevice(r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.Header.Get("X-Device-ID"))
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if id == "" || token == "" {
		return "", false
	}
	hash := tokenHash(token)
	var storedHash, userID, userStatus, deviceStatus string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(token_hash,''),COALESCE(user_id,''),COALESCE((SELECT status FROM users u WHERE u.id=devices.user_id),'active'),COALESCE(devices.status,'active') FROM devices WHERE id=?`, id).Scan(&storedHash, &userID, &userStatus, &deviceStatus); err != nil || storedHash == "" || !secureTokenEqual(storedHash, hash) {
		return "", false
	}
	if deviceStatus != "active" {
		return "", false
	}
	if userID != "" && userStatus != "active" {
		return "", false
	}
	if userID != "" {
		var active int
		err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM auth_sessions WHERE device_id=? AND token_hash=? AND revoked_at IS NULL AND expires_at>?`, id, hash, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&active)
		if err != nil || active == 0 {
			return "", false
		}
		_, _ = a.store.DB().Exec(`UPDATE auth_sessions SET last_seen_at=? WHERE device_id=? AND token_hash=? AND revoked_at IS NULL`, time.Now().UTC().Format(time.RFC3339Nano), id, hash)
	}
	_, _ = a.store.DB().Exec("UPDATE devices SET last_seen_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), id)
	return id, true
}

func (a *API) allowMessage(deviceID string) bool {
	const accountMessagesPerMinute = 300
	now := time.Now()
	a.rateMu.Lock()
	r := a.rates[deviceID]
	allowed := true
	if r.started.IsZero() || now.Sub(r.started) >= time.Minute {
		a.rates[deviceID] = messageRate{started: now, count: 1}
	} else if r.count >= messagesPerMinute {
		allowed = false
	} else {
		r.count++
		a.rates[deviceID] = r
	}
	if len(a.rates) > 10000 {
		for id, state := range a.rates {
			if now.Sub(state.started) >= time.Minute {
				delete(a.rates, id)
			}
		}
	}
	a.rateMu.Unlock()
	if !allowed {
		return false
	}
	return a.allowPersistentSecurityAction("message-device", deviceID, messagesPerMinute, time.Minute) &&
		a.allowPersistentMessageAccount(deviceID, accountMessagesPerMinute)
}

func (a *API) allowPersistentMessageAccount(deviceID string, max int) bool {
	var accountID string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(user_id,'') FROM devices WHERE id=?`, deviceID).Scan(&accountID); err != nil || strings.TrimSpace(accountID) == "" {
		return true
	}
	return a.allowPersistentSecurityAction("message-account", accountID, max, time.Minute)
}

func (a *API) allowPresenceUpdate(deviceID string) bool {
	return a.allowDeviceAction("presence", deviceID, 30, 120)
}

func (a *API) allowCallHeartbeat(deviceID string) bool {
	return a.allowDeviceAction("call-heartbeat", deviceID, 60, 240)
}

func (a *API) allowCallSignaling(deviceID string) bool {
	return a.allowDeviceAction("call-signaling", deviceID, 60, 180)
}

func registrationKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if strings.TrimSpace(r.RemoteAddr) != "" {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return "unknown"
}

func (a *API) allowRegistration(key string) bool {
	const attemptsPerMinute = 5
	now := time.Now()
	a.regRateMu.Lock()
	defer a.regRateMu.Unlock()
	r := a.regRates[key]
	if r.started.IsZero() || now.Sub(r.started) >= time.Minute {
		a.regRates[key] = messageRate{started: now, count: 1}
		if len(a.regRates) > 10000 {
			for k, state := range a.regRates {
				if now.Sub(state.started) >= time.Minute {
					delete(a.regRates, k)
				}
			}
		}
		return true
	}
	if r.count >= attemptsPerMinute {
		return false
	}
	r.count++
	a.regRates[key] = r
	return true
}

func decodeJSON(r *http.Request, dst any, maxBytes int64) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return errors.New("request body too large")
	}
	de := json.NewDecoder(bytes.NewReader(data))
	de.DisallowUnknownFields()
	if err := de.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := de.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	setSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": message})
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func secureTokenEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
