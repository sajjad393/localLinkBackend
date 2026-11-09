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
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bundle 230: new and changed server passwords use 600000 rounds (OWASP 2023
// PBKDF2-HMAC-SHA256 advice). Old hashes keep their stored round count and
// still verify; a successful login quietly re-hashes them (rehashPassword230).
// Phones keep their own 180000 (lib/core/crypto/password_kdf.dart); hashes a
// phone sends (join relay, reset by hash) keep the count the phone sent.
var passwordIterations = 600000

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
		HardwareFP  string `json:"hardware_fp"`
		// Bundle 218: the phone-only account this registration comes from,
		// and the recovery code that phone already shows (kept by the server).
		OfflineUserID string `json:"offline_user_id"`
		RecoveryCode  string `json:"recovery_code"`
		// Bundle 236 (B2): an account made offline whose plain password is
		// no longer on the phone sends its saved password lock instead.
		PasswordHash       string `json:"password_hash"`
		PasswordSalt       string `json:"password_salt"`
		PasswordIterations int    `json:"password_iterations"`
	}
	if err := decodeJSON(r, &in, 12<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	offline := offlineID218(in.OfflineUserID)
	hardwareFP := strings.TrimSpace(in.HardwareFP)
	if len(hardwareFP) > 128 || hasControlChars(hardwareFP) {
		hardwareFP = ""
	}
	lock, lockMode, lockOK := registerLock236(in.Password, in.PasswordHash, in.PasswordSalt, in.PasswordIterations, offline)
	if !lockOK {
		writeError(w, http.StatusBadRequest, "invalid username, password, device id, or device name")
		return
	}
	passwordIn := in.Password
	if lockMode {
		passwordIn = lockPlaceholder236 // only for the shared name / device checks below
	}
	username, password, deviceID, deviceName, platform, pairingCode, ok := normalizeAuthInput(in.Username, passwordIn, in.DeviceID, in.DeviceName, in.Platform, in.PairingCode)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid username, password, device id, or device name")
		return
	}
	// Bundle 236 (B4): an account made offline does not need the pairing code.
	if required := strings.TrimSpace(getenv("LOCALLINK_PAIRING_CODE", "")); required != "" && offline == "" && !secureTokenEqual(pairingCode, required) {
		writeErrorCode(w, http.StatusForbidden, "PAIRING_REQUIRED", "invalid pairing code")
		return
	}
	// Bundle 235: this very phone already belongs to another account - the
	// person must prove that account in the app; the admin sees the wait.
	if o := a.findDuplicateOwner235(deviceID, hardwareFP, username); o != nil {
		a.noteDuplicateWait235(o, deviceID, hardwareFP, username, "", "register")
		writeDuplicate235(w, o)
		return
	}
	var salt, hash []byte
	iterations := passwordIterations
	if lockMode {
		salt, hash, iterations = lock.salt, lock.hash, lock.iterations
	} else {
		salt = make([]byte, passwordSaltLength)
		if _, err := rand.Read(salt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate password salt")
			return
		}
		hash = derivePasswordKey(password, salt, passwordIterations)
	}
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
	// Bundle 218: keep the code the offline phone already shows.
	if c := normalizeRecoveryCode(in.RecoveryCode); recoveryFormat218.MatchString(c) {
		recoveryCode = c
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
		userID, username, base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(hash), iterations, nowText, nowText, tokenHash(recoveryCode), nowText, nowText); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeErrorCode(w, http.StatusConflict, "USERNAME_TAKEN", "username is already registered")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to create account")
		}
		return
	}
	if _, err = tx.Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash,user_id,hardware_fp) VALUES(?,?,?,?,?,?,?,?)`,
		deviceID, deviceName, platform, nowText, nowText, tokenHash(token), userID, hardwareFP); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			// Bundle 218: name the other account only when the same hardware
			// id proves it is this very phone.
			var owner, fp string
			_ = tx.QueryRow(`SELECT COALESCE(u.username,''),COALESCE(d.hardware_fp,'') FROM devices d LEFT JOIN users u ON u.id=d.user_id WHERE d.id=?`, deviceID).Scan(&owner, &fp)
			writeDeviceTaken218(w, "device id is already registered", owner, fp, hardwareFP)
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
	if offline != "" {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO offline_ids(offline_id,user_id,device_id,linked_at) VALUES(?,?,?,?)`, offline, userID, deviceID, nowText); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to link offline account")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit account")
		return
	}
	if offline != "" {
		a.carryOfflineBlock218(userID, offline, deviceID)
		a.audit("account_from_offline", userID, "offline="+offline)
	}
	if lockMode {
		a.audit("account_register_lock", userID, "mode=lock iterations="+strconv.Itoa(iterations))
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
		HardwareFP  string `json:"hardware_fp"`
		// Bundle 218: the phone-only account that signs in to its own account.
		OfflineUserID string `json:"offline_user_id"`
	}
	if err := decodeJSON(r, &in, 12<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	offline := offlineID218(in.OfflineUserID)
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
	hardwareFP := strings.TrimSpace(in.HardwareFP)
	if len(hardwareFP) > 128 || hasControlChars(hardwareFP) {
		hardwareFP = ""
	}
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
		writeErrorCode(w, http.StatusUnauthorized, "BAD_LOGIN", "invalid username or password")
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
		writeErrorCode(w, http.StatusUnauthorized, "BAD_LOGIN", "invalid username or password")
		return
	}
	a.resetLoginFailures(user.ID)
	// Bundle 230: an old hash (fewer rounds) is replaced now that the password
	// is known to be right. Runs outside any transaction (the KDF is slow and
	// the DB has one connection) and only if nobody changed the hash meanwhile.
	if iterations < passwordIterations {
		a.rehashPassword230("users", user.ID, password, hashB64)
	}

	var deviceOwner sql.NullString
	var deviceStatus string
	err = a.store.DB().QueryRow(`SELECT user_id,COALESCE(status,'active') FROM devices WHERE id=?`, deviceID).Scan(&deviceOwner, &deviceStatus)
	if err == nil {
		if deviceStatus != "active" {
			writeError(w, http.StatusForbidden, "device is revoked and cannot re-authenticate")
			return
		}
		if !deviceOwner.Valid || deviceOwner.String == "" || deviceOwner.String != user.ID {
			var owner, fp string
			_ = a.store.DB().QueryRow(`SELECT COALESCE(u.username,''),COALESCE(d.hardware_fp,'') FROM devices d LEFT JOIN users u ON u.id=d.user_id WHERE d.id=?`, deviceID).Scan(&owner, &fp)
			// Bundle 235: another account on this very phone waits for proof.
			if o := a.findDuplicateOwner235(deviceID, hardwareFP, username); o != nil {
				a.noteDuplicateWait235(o, deviceID, hardwareFP, username, "", "login")
				writeDuplicate235(w, o)
				return
			}
			writeDeviceTaken218(w, "device id belongs to another or legacy device", owner, fp, hardwareFP)
			return
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to inspect device")
		return
	} else {
		// Reinstall dedupe: the app regenerated its per-install device id, but the
		// hardware fingerprint proves this is the same physical phone. Reuse the
		// existing (old) device row instead of inserting a duplicate. Password
		// verification above is the ownership proof, and the bind requires the
		// row to already belong to this user — it cannot claim another account's device.
		if hardwareFP != "" {
			var bound string
			bindErr := a.store.DB().QueryRow(`SELECT id FROM devices WHERE hardware_fp=? AND user_id=? AND COALESCE(status,'active')='active' ORDER BY last_seen_at DESC LIMIT 1`, hardwareFP, user.ID).Scan(&bound)
			if bindErr == nil && bound != "" {
				deviceID = bound
				deviceOwner = sql.NullString{String: user.ID, Valid: true}
				err = nil
			}
		}
		if err != nil && errors.Is(err, sql.ErrNoRows) {
			// First-time login from a new device is a device-enrollment event.
			// Require the deployment pairing code when the server is configured with one.
			if required := strings.TrimSpace(getenv("LOCALLINK_PAIRING_CODE", "")); required != "" && !secureTokenEqual(required, pairingCode) {
				writeErrorCode(w, http.StatusForbidden, "PAIRING_REQUIRED", "device pairing is required for this first login")
				return
			}
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
		if _, err = tx.Exec(`UPDATE devices SET name=?,platform=?,last_seen_at=?,token_hash=?,hardware_fp=CASE WHEN ?='' THEN hardware_fp ELSE ? END WHERE id=? AND user_id=?`, deviceName, platform, nowText, tokenHash(token), hardwareFP, hardwareFP, deviceID, user.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update device session")
			return
		}
		_, _ = tx.Exec(`UPDATE auth_sessions SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, nowText, deviceID)
	} else {
		if _, err = tx.Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash,user_id,hardware_fp) VALUES(?,?,?,?,?,?,?,?)`, deviceID, deviceName, platform, nowText, nowText, tokenHash(token), user.ID, hardwareFP); err != nil {
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
	if offline != "" {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO offline_ids(offline_id,user_id,device_id,linked_at) VALUES(?,?,?,?)`, offline, user.ID, deviceID, nowText); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to link offline account")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit login")
		return
	}
	if offline != "" {
		a.carryOfflineBlock218(user.ID, offline, deviceID)
		a.audit("account_link_offline", user.ID, "offline="+offline)
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
	a.endDeviceAdminSessions(deviceID) // Bundle 214
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
	// Bundle 230: one HMAC is reused (Reset + Sum into the same buffer), so a
	// round costs no new allocation. Same bytes as before, about 3x faster.
	mac := hmac.New(sha256.New, []byte(password))
	block := make([]byte, len(salt)+4)
	copy(block, salt)
	binary.BigEndian.PutUint32(block[len(salt):], 1)
	_, _ = mac.Write(block)
	u := mac.Sum(make([]byte, 0, sha256.Size))
	t := append([]byte(nil), u...)
	for i := 2; i <= iterations; i++ {
		mac.Reset()
		_, _ = mac.Write(u)
		u = mac.Sum(u[:0])
		for j := range t {
			t[j] ^= u[j]
		}
	}
	return t[:passwordKeyLength]
}

// rehashPassword230 (Bundle 230) stores a fresh salt and a passwordIterations
// hash for a password that was just verified. table is "users" or
// "admin_accounts" (fixed strings, never user input). The WHERE on the old
// hash makes it a no-op if a reset or another login changed it meanwhile.
// Failures are ignored: the old hash keeps working.
func (a *API) rehashPassword230(table, id, password, oldHashB64 string) {
	if table != "users" && table != "admin_accounts" {
		return
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return
	}
	hash := derivePasswordKey(password, salt, passwordIterations)
	_, _ = a.store.DB().Exec(`UPDATE `+table+` SET password_salt=?,password_hash=?,password_iterations=?,updated_at=? WHERE id=? AND password_hash=?`,
		base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(hash), passwordIterations,
		time.Now().UTC().Format(time.RFC3339Nano), id, oldHashB64)
}
