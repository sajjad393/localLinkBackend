package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var passwordIterations = 180000

const (
	passwordKeyLength  = 32
	passwordSaltLength = 16
	authSessionTTL     = 30 * 24 * time.Hour
	maxUsernameLen     = 32
	maxPasswordLen     = 128
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

type Account struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	CreatedAt string `json:"created_at"`
}

type AuthResponse struct {
	Account      Account `json:"account"`
	Device       Device  `json:"device"`
	Token        string  `json:"token"`
	ExpiresAt    string  `json:"expires_at"`
	RecoveryCode string  `json:"recovery_code,omitempty"`
}

func (a *API) RegisterAccount(w http.ResponseWriter, r *http.Request) {
	if !a.allowRegistration(registrationKey(r)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "registration rate limit exceeded")
		return
	}
	var in struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DeviceID    string `json:"device_id"`
		DeviceName  string `json:"device_name"`
		Platform    string `json:"platform"`
		PairingCode string `json:"pairing_code"`
	}
	if err := decodeJSON(r, &in, 12<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username, password, deviceID, deviceName, platform, pairingCode, ok := normalizeAuthInput(in.Username, in.Password, in.DeviceID, in.DeviceName, in.Platform, in.PairingCode)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid username, password, device id, or device name")
		return
	}
	if required := strings.TrimSpace(getenv("LOCALLINK_PAIRING_CODE", "")); required != "" && pairingCode != required {
		writeError(w, http.StatusForbidden, "invalid pairing code")
		return
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate password salt")
		return
	}
	hash := derivePasswordKey(password, salt, passwordIterations)
	token, err := newToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate device credentials")
		return
	}
	recoveryCode, err := newRecoveryCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate recovery code")
		return
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(authSessionTTL)
	userID := uuid.NewString()
	sessionID := uuid.NewString()

	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start account transaction")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at,recovery_code_hash,recovery_code_created_at,recovery_code_rotated_at,recovery_code_enabled) VALUES(?,?,?,?,?,'active',?,?,?,?,?,1)`,
		userID, username, base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(hash), passwordIterations, nowText, nowText, tokenHash(recoveryCode), nowText, nowText); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "username is already registered")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to create account")
		}
		return
	}
	if _, err = tx.Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash,user_id) VALUES(?,?,?,?,?,?,?)`,
		deviceID, deviceName, platform, nowText, nowText, tokenHash(token), userID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "device id is already registered")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to register device")
		}
		return
	}
	if _, err = tx.Exec(`INSERT INTO auth_sessions(id,user_id,device_id,token_hash,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?)`,
		sessionID, userID, deviceID, tokenHash(token), nowText, nowText, expires.Format(time.RFC3339Nano)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create login session")
		return
	}
	if _, err = tx.Exec(`INSERT INTO profiles(user_id,display_name,avatar_path,avatar_content_type,avatar_size,avatar_sha256,avatar_updated_at,created_at,updated_at) VALUES(?,?,NULL,NULL,0,'',NULL,?,?)`, userID, deviceName, nowText, nowText); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create profile")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit account")
		return
	}
	a.audit("account_register", userID, "username="+username)
	writeJSON(w, http.StatusCreated, AuthResponse{
		Account:      Account{ID: userID, Username: username, CreatedAt: nowText},
		Device:       Device{ID: deviceID, Name: deviceName, Platform: platform, CreatedAt: nowText, LastSeenAt: nowText},
		Token:        token,
		ExpiresAt:    expires.Format(time.RFC3339Nano),
		RecoveryCode: recoveryCode,
	})
}

func (a *API) LoginAccount(w http.ResponseWriter, r *http.Request) {
	if !a.allowSecurityAction("login-ip", registrationKey(r), maxLoginAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "authentication rate limit exceeded")
		return
	}
	var in struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DeviceID    string `json:"device_id"`
		DeviceName  string `json:"device_name"`
		Platform    string `json:"platform"`
		PairingCode string `json:"pairing_code"`
	}
	if err := decodeJSON(r, &in, 12<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if !a.allowSecurityAction("login-account", authUsernameRateKey(r, username), maxLoginAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "authentication rate limit exceeded")
		return
	}
	password := in.Password
	deviceID := strings.TrimSpace(in.DeviceID)
	deviceName := strings.TrimSpace(in.DeviceName)
	platform := strings.TrimSpace(in.Platform)
	pairingCode := strings.TrimSpace(in.PairingCode)
	if !validUsername(username) || len(password) < 8 || len(password) > maxPasswordLen || hasControlChars(password) || len(deviceID) > 128 || hasControlChars(deviceID) || deviceName == "" || len(deviceName) > maxDeviceName || hasControlChars(deviceName) {
		writeError(w, http.StatusBadRequest, "invalid username, password, device id, or device name")
		return
	}
	if deviceID == "" {
		deviceID = uuid.NewString()
	}
	if platform == "" {
		platform = "android"
	}

	var user Account
	var saltB64, hashB64 string
	var iterations int
	var status string
	if err := a.store.DB().QueryRow(`SELECT id,username,created_at,password_salt,password_hash,password_iterations,status FROM users WHERE username=?`, username).Scan(&user.ID, &user.Username, &user.CreatedAt, &saltB64, &hashB64, &iterations, &status); err != nil {
		// Perform the same expensive KDF on unknown accounts to reduce username
		// enumeration by response timing.
		_ = derivePasswordKey(password, []byte("locallink-dummy-salt-v1"), passwordIterations)
		a.auditSecurity("login_failed", "", "", registrationKey(r), "unknown account")
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if status != "active" {
		_ = derivePasswordKey(password, []byte("locallink-dummy-salt-v1"), passwordIterations)
		a.auditSecurity("login_failed", user.ID, "", registrationKey(r), "inactive account")
		writeError(w, http.StatusForbidden, "account is not active")
		return
	}
	if locked := a.loginLockedUntil(user.ID); !locked.IsZero() {
		seconds := int(time.Until(locked).Seconds())
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", itoa(seconds))
		writeError(w, http.StatusTooManyRequests, "account temporarily locked after repeated failed logins")
		return
	}
	salt, err := base64.RawURLEncoding.DecodeString(saltB64)
	if err != nil || len(salt) != passwordSaltLength {
		writeError(w, http.StatusInternalServerError, "stored account credentials are invalid")
		return
	}
	expected, err := base64.RawURLEncoding.DecodeString(hashB64)
	if err != nil || len(expected) != passwordKeyLength {
		writeError(w, http.StatusInternalServerError, "stored account credentials are invalid")
		return
	}
	actual := derivePasswordKey(password, salt, iterations)
	if !hmac.Equal(actual, expected) {
		lockedUntil := a.recordLoginFailure(user.ID)
		a.auditSecurity("login_failed", user.ID, "", registrationKey(r), "invalid password")
		if !lockedUntil.IsZero() {
			seconds := int(time.Until(lockedUntil).Seconds())
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", itoa(seconds))
			writeError(w, http.StatusTooManyRequests, "account temporarily locked after repeated failed logins")
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	a.resetLoginFailures(user.ID)

	var deviceOwner sql.NullString
	var deviceStatus string
	err = a.store.DB().QueryRow(`SELECT user_id,COALESCE(status,'active') FROM devices WHERE id=?`, deviceID).Scan(&deviceOwner, &deviceStatus)
	if err == nil {
		if deviceStatus != "active" {
			writeError(w, http.StatusForbidden, "device is revoked and cannot re-authenticate")
			return
		}
		if !deviceOwner.Valid || deviceOwner.String == "" || deviceOwner.String != user.ID {
			writeError(w, http.StatusConflict, "device id belongs to another or legacy device")
			return
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to inspect device")
		return
	} else {
		// First-time login from a new device is a device-enrollment event.
		// Require the deployment pairing code when the server is configured with one.
		if required := strings.TrimSpace(getenv("LOCALLINK_PAIRING_CODE", "")); required != "" && !secureTokenEqual(required, pairingCode) {
			writeError(w, http.StatusForbidden, "device pairing is required for this first login")
			return
		}
	}

	token, err := newToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate session")
		return
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(authSessionTTL)
	sessionID := uuid.NewString()

	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start login transaction")
		return
	}
	defer tx.Rollback()
	if err == nil && deviceOwner.Valid {
		if _, err = tx.Exec(`UPDATE devices SET name=?,platform=?,last_seen_at=?,token_hash=? WHERE id=? AND user_id=?`, deviceName, platform, nowText, tokenHash(token), deviceID, user.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update device session")
			return
		}
		_, _ = tx.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, nowText, deviceID)
	} else {
		if _, err = tx.Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash,user_id) VALUES(?,?,?,?,?,?,?)`, deviceID, deviceName, platform, nowText, nowText, tokenHash(token), user.ID); err != nil {
			writeError(w, http.StatusConflict, "device registration conflict")
			return
		}
	}
	if _, err = tx.Exec(`INSERT INTO auth_sessions(id,user_id,device_id,token_hash,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?)`, sessionID, user.ID, deviceID, tokenHash(token), nowText, nowText, expires.Format(time.RFC3339Nano)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create login session")
		return
	}
	if _, err = tx.Exec(`INSERT INTO profiles(user_id,display_name,avatar_path,avatar_content_type,avatar_size,avatar_sha256,avatar_updated_at,created_at,updated_at) VALUES(?,?,NULL,NULL,0,'',NULL,?,?) ON CONFLICT(user_id) DO NOTHING`, user.ID, deviceName, nowText, nowText); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize profile")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit login")
		return
	}
	a.audit("account_login", user.ID, "device="+deviceID)
	a.auditSecurity("login_success", user.ID, deviceID, registrationKey(r), "authenticated")
	writeJSON(w, http.StatusOK, AuthResponse{
		Account:   user,
		Device:    Device{ID: deviceID, Name: deviceName, Platform: platform},
		Token:     token,
		ExpiresAt: expires.Format(time.RFC3339Nano),
	})
}

func (a *API) AuthMe(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var account Account
	if err := a.store.DB().QueryRow(`SELECT u.id,u.username,u.created_at FROM users u JOIN devices d ON d.user_id=u.id WHERE d.id=? AND u.status='active'`, deviceID).Scan(&account.ID, &account.Username, &account.CreatedAt); err != nil {
		writeError(w, http.StatusNotFound, "account is not linked to this device")
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (a *API) LogoutAccount(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = a.store.DB().Exec(`UPDATE auth_sessions SET revoked_at=? WHERE device_id=? AND token_hash=? AND revoked_at IS NULL`, now, deviceID, tokenHash(token))
	_, _ = a.store.DB().Exec(`UPDATE devices SET token_hash=NULL WHERE id=?`, deviceID)
	a.hub.CloseDevice(deviceID)
	d, _ := a.setPresence(deviceID, false, false, DevicePresence{})
	a.broadcastPresence(deviceID, d)
	a.audit("account_logout", deviceID, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func newRecoveryCode() (string, error) {
	b := make([]byte, 15)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	enc := strings.ToUpper(base64.RawStdEncoding.EncodeToString(b))
	// Convert the compact random value into a human-friendly, case-insensitive code.
	enc = strings.NewReplacer("+", "A", "/", "B").Replace(enc)
	if len(enc) < 20 {
		return "", errors.New("failed to generate recovery code")
	}
	return enc[:5] + "-" + enc[5:10] + "-" + enc[10:15] + "-" + enc[15:20], nil
}

func normalizeAuthInput(username, password, deviceID, deviceName, platform, pairingCode string) (string, string, string, string, string, string, bool) {
	username = strings.ToLower(strings.TrimSpace(username))
	deviceID = strings.TrimSpace(deviceID)
	deviceName = strings.TrimSpace(deviceName)
	platform = strings.TrimSpace(platform)
	pairingCode = strings.TrimSpace(pairingCode)
	if !validUsername(username) || len(password) < 8 || len(password) > maxPasswordLen || hasControlChars(password) {
		return "", "", "", "", "", "", false
	}
	if deviceID == "" {
		deviceID = uuid.NewString()
	}
	if len(deviceID) > 128 || hasControlChars(deviceID) || deviceName == "" || len(deviceName) > maxDeviceName || hasControlChars(deviceName) {
		return "", "", "", "", "", "", false
	}
	if platform == "" {
		platform = "android"
	}
	return username, password, deviceID, deviceName, platform, pairingCode, true
}

func validUsername(value string) bool {
	return len(value) >= 3 && len(value) <= maxUsernameLen && usernamePattern.MatchString(value)
}

func derivePasswordKey(password string, salt []byte, iterations int) []byte {
	if iterations < 1 {
		iterations = passwordIterations
	}
	// PBKDF2-HMAC-SHA256, block 1. Implemented locally to keep the backend dependency-light.
	h := func(data []byte) []byte {
		mac := hmac.New(sha256.New, []byte(password))
		_, _ = mac.Write(data)
		return mac.Sum(nil)
	}
	block := make([]byte, len(salt)+4)
	copy(block, salt)
	binary.BigEndian.PutUint32(block[len(salt):], 1)
	u := h(block)
	t := append([]byte(nil), u...)
	for i := 2; i <= iterations; i++ {
		u = h(u)
		for j := range t {
			t[j] ^= u[j]
		}
	}
	return t[:passwordKeyLength]
}
