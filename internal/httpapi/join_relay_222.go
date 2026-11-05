package httpapi

// Bundle 222: nearby phones carry an offline account to the server.
//
// Her phone (made offline, never online) seals a "join box" for the server
// (llaa1 box to the server's answer key from bundle 221). The box holds her
// name, her password HASH (PBKDF2-SHA256, same as the server), the HASH of her
// recovery code and her phone keys - never the password, never the code.
// Any phone that meets her carries the box; the first one with internet
// uploads it here. The server applies the 218 rules itself (there is no app
// on the line to retry):
//   - name free                         -> new account (status "joined")
//   - name taken, same recovery code    -> her own account, phone joins ("linked")
//   - name taken by someone else        -> silent rename sumaira37 ("renamed")
//   - phone id belongs to someone else  -> "refused" + Audit (stays local)
// The answer is server-signed and carries her session token locked for HER
// phone, so it can travel back phone to phone. Same box twice -> same answer.
//
// Phones also report offline accounts they saw nearby (presence_update
// "offline_seen"), so the web Users page can list "Not on server yet".

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	joinBoxMaxLen222     = 12 << 10 // base64 of an 8 KB box
	joinAnswerKeep222    = 60 * 24 * time.Hour
	offlineSeenKeep222   = 60 * 24 * time.Hour
	maxOfflineSeenPerMsg = 20
)

type joinBoxBody222 struct {
	V             int    `json:"v"`
	BoxID         string `json:"box_id"`
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	OfflineUserID string `json:"offline_user_id"`
	DeviceID      string `json:"device_id"`
	DeviceName    string `json:"device_name"`
	Platform      string `json:"platform"`
	HardwareFP    string `json:"hardware_fp"`
	PwdSalt       string `json:"pwd_salt"`
	PwdHash       string `json:"pwd_hash"`
	Iterations    int    `json:"iterations"`
	RecoveryHash  string `json:"recovery_hash"`
	IdentityKey   string `json:"identity_key"`
	SigningKey    string `json:"signing_key"`
	PairingCode   string `json:"pairing_code"`
	MadeAt        string `json:"made_at"`
}

type joinAnswerBody222 struct {
	V             int    `json:"v"`
	Kind          string `json:"kind"` // "join_answer"
	BoxID         string `json:"box_id"`
	DeviceID      string `json:"device_id"`
	OfflineUserID string `json:"offline_user_id"`
	Status        string `json:"status"` // joined | linked | renamed | already | refused
	Reason        string `json:"reason,omitempty"`
	Username      string `json:"username,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	TokenBox      string `json:"token_box,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	At            string `json:"at"`
}

// offlineSeen222 is one offline account a phone saw nearby.
type offlineSeen222 struct {
	DeviceID string `json:"device_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Model    string `json:"model"`
	Link     string `json:"link"`
	Carrying bool   `json:"carrying"`
}

// joinNote222 is the server-signed note that tells a never-online phone the
// server's box key. The body has no time in it, so every phone hands out
// exactly the same note (Ed25519 signatures are deterministic).
func (a *API) joinNote222() (signedBlob, string, error) {
	key, err := a.serverSigningKey()
	if err != nil {
		return signedBlob{}, "", err
	}
	_, joinPub, err := a.answerBoxKey()
	if err != nil {
		return signedBlob{}, "", err
	}
	pub := key.Public().(ed25519.PublicKey)
	body, _ := json.Marshal(map[string]any{"v": 1, "kind": "join_key", "server_key": b64url(pub), "key_id": serverKeyID(pub), "join_key": joinPub})
	blob, err := a.serverSign(string(body))
	return blob, joinPub, err
}

func validJoinBox222(b joinBoxBody222) bool {
	salt, e1 := unb64url(b.PwdSalt)
	hash, e2 := unb64url(b.PwdHash)
	rh, e3 := hex.DecodeString(b.RecoveryHash)
	sk, e4 := unb64url(b.SigningKey)
	return b.V == 1 && b.BoxID != "" && len(b.BoxID) <= 128 && !hasControlChars(b.BoxID) &&
		validUsername(strings.ToLower(strings.TrimSpace(b.Username))) &&
		b.DeviceID != "" && len(b.DeviceID) <= 128 && !hasControlChars(b.DeviceID) &&
		len(b.DeviceName) <= maxDeviceName && !hasControlChars(b.DeviceName) && len(b.DisplayName) <= 80 && !hasControlChars(b.DisplayName) &&
		e1 == nil && len(salt) == passwordSaltLength && e2 == nil && len(hash) == passwordKeyLength &&
		b.Iterations >= minResetIterations && b.Iterations <= maxResetIterations &&
		e3 == nil && len(rh) == 32 && validIdentityPublicKey(b.IdentityKey) && e4 == nil && len(sk) == ed25519.PublicKeySize &&
		len(b.HardwareFP) <= 128 && !hasControlChars(b.HardwareFP) && len(b.PairingCode) <= 128
}

func renameCandidate222(base string) string {
	n, _ := rand.Int(rand.Reader, big.NewInt(98))
	return base + itoa(int(n.Int64())+2)
}

// JoinRelay222 is POST /api/v1/auth/join-relay (a signed-in carrier uploads a box).
func (a *API) JoinRelay222(w http.ResponseWriter, r *http.Request) {
	carrier, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !a.allowPersistentSecurityAction("join-relay", carrier, 30, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeErrorCode(w, http.StatusTooManyRequests, "RATE_LIMIT", "too many join boxes")
		return
	}
	var in struct {
		BoxID string `json:"box_id"`
		Box   string `json:"box"`
	}
	if err := decodeJSON(r, &in, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.BoxID = strings.TrimSpace(in.BoxID)
	if in.BoxID == "" || len(in.BoxID) > 128 || hasControlChars(in.BoxID) || in.Box == "" || len(in.Box) > joinBoxMaxLen222 {
		writeErrorCode(w, http.StatusBadRequest, "BAD_JOIN_BOX", "invalid join box")
		return
	}
	// Same box twice -> same answer.
	var stored string
	if err := a.store.DB().QueryRow(`SELECT answer FROM join_relay_answers WHERE box_id=?`, in.BoxID).Scan(&stored); err == nil {
		_, _ = a.store.DB().Exec(`UPDATE join_relay_answers SET carriers=carriers+1 WHERE box_id=?`, in.BoxID)
		var blob signedBlob
		_ = json.Unmarshal([]byte(stored), &blob)
		writeJSON(w, http.StatusOK, map[string]any{"answer": blob, "again": true})
		return
	}
	priv, _, err := a.answerBoxKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server box key unavailable")
		return
	}
	plain, err := openBox(priv, in.Box)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "BAD_JOIN_BOX", "join box can't be opened")
		return
	}
	var sealed signedBlob
	var b joinBoxBody222
	if json.Unmarshal(plain, &sealed) != nil || json.Unmarshal([]byte(sealed.Body), &b) != nil || !validJoinBox222(b) || b.BoxID != in.BoxID || !verifyWithKey(b.SigningKey, sealed) {
		writeErrorCode(w, http.StatusBadRequest, "BAD_JOIN_BOX", "join box is not valid")
		return
	}
	answer := a.applyJoinBox222(b, carrier)
	body, _ := json.Marshal(answer)
	blob, err := a.serverSign(string(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign join answer")
		return
	}
	raw, _ := json.Marshal(blob)
	_, _ = a.store.DB().Exec(`INSERT OR IGNORE INTO join_relay_answers(box_id,device_id,user_id,status,answer,carriers,made_at) VALUES(?,?,?,?,?,1,?)`,
		b.BoxID, b.DeviceID, answer.UserID, answer.Status, string(raw), answer.At)
	if answer.Status != "refused" {
		_, _ = a.store.DB().Exec(`DELETE FROM seen_offline_accounts WHERE device_id=?`, b.DeviceID)
	}
	_, _ = a.store.DB().Exec(`DELETE FROM join_relay_answers WHERE made_at < ?`, time.Now().UTC().Add(-joinAnswerKeep222).Format(time.RFC3339Nano))
	writeJSON(w, http.StatusOK, map[string]any{"answer": blob})
}

// applyJoinBox222 does the join and returns the (unsigned) answer.
func (a *API) applyJoinBox222(b joinBoxBody222, carrier string) joinAnswerBody222 {
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	ans := joinAnswerBody222{V: 1, Kind: "join_answer", BoxID: b.BoxID, DeviceID: b.DeviceID, OfflineUserID: b.OfflineUserID, At: nowText}
	refuse := func(reason string) joinAnswerBody222 {
		ans.Status, ans.Reason = "refused", reason
		a.audit("join_relay_refused", "", "device="+b.DeviceID+" reason="+reason+" carrier="+carrier)
		return ans
	}
	offline := offlineID218(b.OfflineUserID)
	username := strings.ToLower(strings.TrimSpace(b.Username))
	deviceName := strings.TrimSpace(b.DeviceName)
	if deviceName == "" {
		deviceName = "Phone"
	}
	platform := strings.TrimSpace(b.Platform)
	if platform == "" {
		platform = "android"
	}
	display := strings.TrimSpace(b.DisplayName)
	if display == "" {
		display = username
	}
	if required := strings.TrimSpace(getenv("LOCALLINK_PAIRING_CODE", "")); required != "" && !secureTokenEqual(required, strings.TrimSpace(b.PairingCode)) {
		return refuse("pairing")
	}
	// The phone id: free, hers already, or someone else's (stays local + Audit).
	var owner sql.NullString
	var devStatus string
	devErr := a.store.DB().QueryRow(`SELECT user_id,COALESCE(status,'active') FROM devices WHERE id=?`, b.DeviceID).Scan(&owner, &devStatus)
	var linkedUser string
	if offline != "" {
		_ = a.store.DB().QueryRow(`SELECT user_id FROM offline_ids WHERE offline_id=?`, offline).Scan(&linkedUser)
	}
	if devErr == nil {
		if devStatus != "active" {
			return refuse("device_revoked")
		}
		if !owner.Valid || owner.String == "" || (linkedUser != "" && owner.String != linkedUser) || (linkedUser == "" && !a.sameRecovery222(owner.String, b.RecoveryHash)) {
			a.auditSecurity("join_relay_device_taken", owner.String, b.DeviceID, "relay:"+carrier, "phone id belongs to another account")
			return refuse("device_taken")
		}
		// Her phone is already on the server (she joined by herself or by an
		// earlier box). A token only when that phone has no live sign-in, so
		// an answer arriving late never signs her out.
		ans.Status, ans.UserID = "already", owner.String
		ans.Username = a.usernameOf(owner.String)
		var live int
		_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM auth_sessions WHERE device_id=? AND revoked_at IS NULL AND expires_at > ?`, b.DeviceID, nowText).Scan(&live)
		if live == 0 {
			a.lockJoinToken222(&ans, b, owner.String, deviceName, platform, now)
		}
		return ans
	} else if !errors.Is(devErr, sql.ErrNoRows) {
		return refuse("server")
	}

	tx, err := a.store.DB().Begin()
	if err != nil {
		return refuse("server")
	}
	defer tx.Rollback()
	userID, status := "", "joined"
	name := username
	for attempt := 0; attempt < 12; attempt++ {
		var existing, recHash string
		err := tx.QueryRow(`SELECT id,COALESCE(recovery_code_hash,'') FROM users WHERE username=?`, name).Scan(&existing, &recHash)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return refuse("server")
		}
		if attempt == 0 && (existing == linkedUser || (recHash != "" && secureTokenEqual(recHash, strings.ToLower(b.RecoveryHash)))) {
			// Same name + her own recovery code: her account; this phone joins it.
			userID, status = existing, "linked"
			break
		}
		name, status = renameCandidate222(username), "renamed"
		if !validUsername(name) {
			return refuse("name")
		}
	}
	if userID == "" {
		userID = uuid.NewString()
		if _, err := tx.Exec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at,recovery_code_hash,recovery_code_created_at,recovery_code_rotated_at,recovery_code_enabled) VALUES(?,?,?,?,?,'active',?,?,?,?,?,1)`,
			userID, name, b.PwdSalt, b.PwdHash, b.Iterations, nowText, nowText, strings.ToLower(b.RecoveryHash), nowText, nowText); err != nil {
			return refuse("name")
		}
		if _, err := tx.Exec(`INSERT INTO profiles(user_id,display_name,avatar_path,avatar_content_type,avatar_size,avatar_sha256,avatar_updated_at,created_at,updated_at) VALUES(?,?,NULL,NULL,0,'',NULL,?,?) ON CONFLICT(user_id) DO NOTHING`, userID, display, nowText, nowText); err != nil {
			return refuse("server")
		}
	}
	if _, err := tx.Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash,user_id,hardware_fp,identity_public_key) VALUES(?,?,?,?,?,NULL,?,?,?)`,
		b.DeviceID, deviceName, platform, nowText, nowText, userID, b.HardwareFP, b.IdentityKey); err != nil {
		return refuse("device_taken")
	}
	if err := ensureIdentityKeyHistory(tx, b.DeviceID, b.IdentityKey, 1, nowText); err != nil {
		return refuse("server")
	}
	if offline != "" {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO offline_ids(offline_id,user_id,device_id,linked_at) VALUES(?,?,?,?)`, offline, userID, b.DeviceID, nowText); err != nil {
			return refuse("server")
		}
	}
	if err := tx.Commit(); err != nil {
		return refuse("server")
	}
	a.rememberSigningKey(b.DeviceID, b.SigningKey)
	if offline != "" {
		a.carryOfflineBlock218(userID, offline, b.DeviceID)
	}
	ans.Status, ans.UserID, ans.Username = status, userID, name
	a.lockJoinToken222(&ans, b, userID, deviceName, platform, now)
	a.audit("account_joined_by_relay", userID, "username="+name+" status="+status+" device="+b.DeviceID+" carrier="+carrier)
	return ans
}

func (a *API) sameRecovery222(userID, recoveryHash string) bool {
	var h string
	_ = a.store.DB().QueryRow(`SELECT COALESCE(recovery_code_hash,'') FROM users WHERE id=?`, userID).Scan(&h)
	return h != "" && secureTokenEqual(h, strings.ToLower(recoveryHash))
}

// lockJoinToken222 makes a sign-in for her phone and locks the token for her
// identity key, so only her phone can use it.
func (a *API) lockJoinToken222(ans *joinAnswerBody222, b joinBoxBody222, userID, deviceName, platform string, now time.Time) {
	token, err := newToken()
	if err != nil {
		return
	}
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(authSessionTTL).Format(time.RFC3339Nano)
	plain, _ := json.Marshal(map[string]string{"token": token, "expires_at": expires, "device_id": b.DeviceID, "user_id": userID, "username": ans.Username})
	box, err := lockBox(b.IdentityKey, plain)
	if err != nil {
		return
	}
	if _, err := a.store.DB().Exec(`UPDATE devices SET token_hash=?,last_seen_at=?,name=?,platform=? WHERE id=? AND user_id=?`, tokenHash(token), nowText, deviceName, platform, b.DeviceID, userID); err != nil {
		return
	}
	if _, err := a.store.DB().Exec(`INSERT INTO auth_sessions(id,user_id,device_id,token_hash,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?)`, uuid.NewString(), userID, b.DeviceID, tokenHash(token), nowText, nowText, expires); err != nil {
		return
	}
	ans.TokenBox, ans.ExpiresAt = box, expires
}

// noteOfflineSeen222 keeps the offline accounts a phone saw nearby.
func (a *API) noteOfflineSeen222(reporter string, list []offlineSeen222) {
	if reporter == "" || len(list) == 0 {
		return
	}
	if len(list) > maxOfflineSeenPerMsg {
		list = list[:maxOfflineSeenPerMsg]
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	clip := func(s string, n int) string {
		s = strings.TrimSpace(s)
		if hasControlChars(s) {
			return ""
		}
		if len(s) > n {
			return s[:n]
		}
		return s
	}
	for _, x := range list {
		dev := clip(x.DeviceID, 128)
		if dev == "" || !strings.HasPrefix(strings.TrimSpace(x.UserID), "offline_") {
			continue
		}
		var joined int
		_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM devices WHERE id=? AND user_id IS NOT NULL AND user_id<>''`, dev).Scan(&joined)
		if joined > 0 {
			_, _ = a.store.DB().Exec(`DELETE FROM seen_offline_accounts WHERE device_id=?`, dev)
			continue
		}
		link := clip(x.Link, 24)
		carriers := ""
		_ = a.store.DB().QueryRow(`SELECT carriers FROM seen_offline_accounts WHERE device_id=?`, dev).Scan(&carriers)
		if x.Carrying && !strings.Contains(","+carriers+",", ","+reporter+",") && strings.Count(carriers, ",") < 9 {
			if carriers == "" {
				carriers = reporter
			} else {
				carriers += "," + reporter
			}
		}
		_, _ = a.store.DB().Exec(`INSERT INTO seen_offline_accounts(device_id,username,name,phone_model,seen_by_device,link,first_seen,last_seen,carriers) VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(device_id) DO UPDATE SET username=CASE WHEN excluded.username='' THEN username ELSE excluded.username END,
			name=CASE WHEN excluded.name='' THEN name ELSE excluded.name END,
			phone_model=CASE WHEN excluded.phone_model='' THEN phone_model ELSE excluded.phone_model END,
			seen_by_device=excluded.seen_by_device, link=excluded.link, last_seen=excluded.last_seen, carriers=excluded.carriers`,
			dev, strings.ToLower(clip(x.Username, 64)), clip(x.Name, 80), clip(x.Model, 64), reporter, link, now, now, carriers)
	}
	_, _ = a.store.DB().Exec(`DELETE FROM seen_offline_accounts WHERE last_seen < ?`, time.Now().UTC().Add(-offlineSeenKeep222).Format(time.RFC3339Nano))
}

// notOnServerRow222 is one web Users row "Not on server yet".
type notOnServerRow222 struct {
	DeviceID  string `json:"device_id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	SeenBy    string `json:"seen_by"`
	Link      string `json:"link"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Carriers  int    `json:"carriers"`
	Status    string `json:"status"`
}

func (a *API) notOnServer222() []notOnServerRow222 {
	rows, err := a.store.DB().Query(`SELECT s.device_id,s.username,s.name,s.phone_model,COALESCE(u.username,s.seen_by_device),s.link,s.first_seen,s.last_seen,s.carriers
		FROM seen_offline_accounts s LEFT JOIN devices d ON d.id=s.seen_by_device LEFT JOIN users u ON u.id=d.user_id
		ORDER BY s.last_seen DESC LIMIT 200`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []notOnServerRow222{}
	for rows.Next() {
		var x notOnServerRow222
		var carriers string
		if rows.Scan(&x.DeviceID, &x.Username, &x.Name, &x.Model, &x.SeenBy, &x.Link, &x.FirstSeen, &x.LastSeen, &carriers) == nil {
			if carriers != "" {
				x.Carriers = strings.Count(carriers, ",") + 1
			}
			x.Status = "not_on_server"
			out = append(out, x)
		}
	}
	return out
}
