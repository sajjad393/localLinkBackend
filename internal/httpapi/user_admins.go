package httpapi

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bundle 214: app users as admins.
//
// A web Owner marks any app user as an admin (with a level). The server keeps
// a linked admin account (username "app:<user id>", no usable password, so
// the web login refuses it). The user's phone asks for its own admin session
// with its normal device sign-in (no admin password in the app), and every
// phone learns who is an admin from small server-signed "admin cards" that
// travel phone to phone like the 212 blocks (the crown on the photo).

const deviceAdminSessionTTL = 30 * 24 * time.Hour

var userAdminRoles = map[string]bool{"owner": true, "operator": true, "security": true, "auditor": true}

type adminBadgeBody struct {
	V        int    `json:"v"`
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	On       bool   `json:"on"`
	Seq      int64  `json:"seq"`
	At       string `json:"at"`
	By       string `json:"by"`
	Signer   string `json:"signer"`
}

// linkedAdmin returns the admin account linked to an app user.
func (a *API) linkedAdmin(userID string) (id, role, status string, ok bool) {
	err := a.store.DB().QueryRow(`SELECT id,role,status FROM admin_accounts WHERE user_id=?`, userID).Scan(&id, &role, &status)
	return id, role, status, err == nil
}

// deviceAdminAllowed: the phone is still signed in to this active account.
func (a *API) deviceAdminAllowed(userID, deviceID string) bool {
	if strings.TrimSpace(deviceID) == "" {
		return false
	}
	var n int
	err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM devices d JOIN users u ON u.id=d.user_id
		WHERE d.id=? AND d.user_id=? AND COALESCE(d.status,'active')='active' AND COALESCE(u.status,'active')='active'
		AND EXISTS(SELECT 1 FROM auth_sessions s WHERE s.device_id=d.id AND s.revoked_at IS NULL AND s.expires_at>?)`,
		deviceID, userID, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&n)
	return err == nil && n > 0
}

// endDeviceAdminSessions ends the admin session (and offline key) of one
// phone: called on logout and device revoke.
func (a *API) endDeviceAdminSessions(deviceID string) {
	if strings.TrimSpace(deviceID) == "" {
		return
	}
	rows, err := a.store.DB().Query(`SELECT id FROM admin_sessions WHERE device_id=? AND revoked_at IS NULL`, deviceID)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, id := range ids {
		_, _ = a.store.DB().Exec(`UPDATE admin_sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, now, id)
		a.endCertsForSessionID(id)
	}
}

func (a *API) activeOwnersExcept(adminID string) int {
	var n int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_accounts WHERE role='owner' AND status='active' AND id<>?`, adminID).Scan(&n)
	return n
}

// currentBadge returns the newest saved card for a user.
func (a *API) currentBadge(userID string) (*adminBadgeBody, *signedBlob) {
	var b signedBlob
	if err := a.store.DB().QueryRow(`SELECT body,sig FROM admin_badges WHERE user_id=?`, userID).Scan(&b.Body, &b.Sig); err != nil {
		return nil, nil
	}
	var rec adminBadgeBody
	if json.Unmarshal([]byte(b.Body), &rec) != nil {
		return nil, nil
	}
	return &rec, &b
}

// serverAdminBadge signs and saves a new card (seq = current + 1).
func (a *API) serverAdminBadge(userID, role string, on bool, by string) (signedBlob, error) {
	var curSeq int64
	_ = a.store.DB().QueryRow(`SELECT seq FROM admin_badges WHERE user_id=?`, userID).Scan(&curSeq)
	var name string
	_ = a.store.DB().QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&name)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rec := adminBadgeBody{V: 1, Kind: "admin_badge", ID: uuid.NewString(), UserID: userID, Username: name,
		Role: role, On: on, Seq: curSeq + 1, At: now, By: by, Signer: "server"}
	raw, _ := json.Marshal(rec)
	blob, err := a.serverSign(string(raw))
	if err != nil {
		return signedBlob{}, err
	}
	onFlag := 0
	if on {
		onFlag = 1
	}
	_, err = a.store.DB().Exec(`INSERT INTO admin_badges(user_id,record_id,role,on_flag,seq,at,body,sig,updated_at) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET record_id=excluded.record_id,role=excluded.role,on_flag=excluded.on_flag,seq=excluded.seq,at=excluded.at,body=excluded.body,sig=excluded.sig,updated_at=excluded.updated_at`,
		userID, rec.ID, role, onFlag, rec.Seq, now, blob.Body, blob.Sig, now)
	return blob, err
}

// notifyAdminRole tells the user's phones to refresh their own role, and
// every connected phone that the admin cards changed.
func (a *API) notifyAdminRole(userID string) {
	own := map[string]bool{}
	if rows, err := a.store.DB().Query(`SELECT id FROM devices WHERE user_id=?`, userID); err == nil {
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				own[id] = true
			}
		}
		rows.Close()
	}
	role := mustJSON(map[string]any{"type": "admin_role_changed"})
	badge := mustJSON(map[string]any{"type": "admin_badge_changed"})
	for _, id := range a.hub.DeviceIDs() {
		if own[id] {
			a.hub.SendTo(id, role)
		}
		a.hub.SendTo(id, badge)
	}
}

// removeLinkedAdmin turns a linked admin off: sessions and offline keys end,
// and a card with on:false removes the crown everywhere.
func (a *API) removeLinkedAdmin(userID, by string) bool {
	adminID, role, status, ok := a.linkedAdmin(userID)
	if !ok || status != "active" {
		return false
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = a.store.DB().Exec(`UPDATE admin_accounts SET status='disabled',updated_at=? WHERE id=?`, now, adminID)
	_, _ = a.store.DB().Exec(`UPDATE admin_sessions SET revoked_at=? WHERE admin_id=? AND revoked_at IS NULL`, now, adminID)
	a.endCertsForAdmin(adminID)
	_, _ = a.serverAdminBadge(userID, role, false, by)
	a.notifyAdminRole(userID)
	a.adminActionForUser("admin_badge", userID, AdminAccount{Username: by}, a.currentBadgeBlob(userID), nil, "admin removed") // Bundle 220
	return true
}

func randomUnusablePassword() (salt, hash string) {
	s := make([]byte, passwordSaltLength)
	h := make([]byte, passwordKeyLength)
	_, _ = io.ReadFull(rand.Reader, s)
	_, _ = io.ReadFull(rand.Reader, h)
	return base64.RawURLEncoding.EncodeToString(s), base64.RawURLEncoding.EncodeToString(h)
}

// AdminSetUserAdmin: web Owner only. Body {user_id, role, remove}.
func (a *API) AdminSetUserAdmin(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_users")
	if !ok {
		return
	}
	if admin.Role != "owner" || admin.UserID != "" {
		writeError(w, http.StatusForbidden, "only a web owner can make or remove admins")
		return
	}
	var in struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
		Remove bool   `json:"remove"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.UserID = strings.TrimSpace(in.UserID)
	in.Role = strings.ToLower(strings.TrimSpace(in.Role))
	if in.Role == "" {
		in.Role = "owner"
	}
	var userStatus string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(status,'active') FROM users WHERE id=?`, in.UserID).Scan(&userStatus); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	adminID, curRole, curStatus, linked := a.linkedAdmin(in.UserID)
	if in.Remove {
		if !linked || curStatus != "active" {
			writeError(w, http.StatusNotFound, "this user is not an admin")
			return
		}
		if curRole == "owner" && a.activeOwnersExcept(adminID) == 0 {
			writeError(w, http.StatusConflict, "can't remove the last owner")
			return
		}
		a.removeLinkedAdmin(in.UserID, admin.Username)
		a.adminAudit(admin.ID, "user_admin_removed", in.UserID, "role="+curRole)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "admin_role": ""})
		return
	}
	if !userAdminRoles[in.Role] {
		writeError(w, http.StatusBadRequest, "role must be owner, operator, security or auditor")
		return
	}
	if userStatus == "disabled" {
		writeError(w, http.StatusConflict, "a disabled account can't be an admin")
		return
	}
	if linked && curStatus == "active" && curRole == "owner" && in.Role != "owner" && a.activeOwnersExcept(adminID) == 0 {
		writeError(w, http.StatusConflict, "can't remove the last owner")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if !linked {
		salt, hash := randomUnusablePassword()
		adminID = uuid.NewString()
		if _, err := a.store.DB().Exec(`INSERT INTO admin_accounts(id,username,password_salt,password_hash,password_iterations,role,status,created_at,updated_at,user_id) VALUES(?,?,?,?,?,?,'active',?,?,?)`,
			adminID, "app:"+in.UserID, salt, hash, passwordIterations, in.Role, now, now, in.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to make admin")
			return
		}
	} else {
		if _, err := a.store.DB().Exec(`UPDATE admin_accounts SET role=?,status='active',updated_at=? WHERE id=?`, in.Role, now, adminID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update admin")
			return
		}
		// A level change ends the old offline keys (they carry the old role);
		// the phone gets a fresh key on its next refresh.
		if curRole != in.Role {
			a.endCertsForAdmin(adminID)
		}
	}
	if _, err := a.serverAdminBadge(in.UserID, in.Role, true, admin.Username); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign admin card")
		return
	}
	a.notifyAdminRole(in.UserID)
	a.adminActionForUser("admin_badge", in.UserID, admin, a.currentBadgeBlob(in.UserID), nil, "role="+in.Role) // Bundle 220
	action := "user_admin_made"
	if linked && curStatus == "active" {
		action = "user_admin_level"
	}
	a.adminAudit(admin.ID, action, in.UserID, "role="+in.Role)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "admin_role": in.Role})
}

// AdminDeviceSession: a signed-in phone asks for its own admin session.
// Answer = same shape as the admin login + the signed admin card.
// Not an admin -> 403 {"error":"not_admin"}.
func (a *API) AdminDeviceSession(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if r.ContentLength != 0 {
		_ = decodeJSON(r, &in, 8<<10)
	}
	var userID string
	_ = a.store.DB().QueryRow(`SELECT COALESCE(user_id,'') FROM devices WHERE id=?`, deviceID).Scan(&userID)
	adminID, role, status, linked := a.linkedAdmin(userID)
	if userID == "" || !linked || status != "active" {
		writeError(w, http.StatusForbidden, "not_admin")
		return
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(deviceAdminSessionTTL).Format(time.RFC3339Nano)
	var sessionID, storedHash string
	err := a.store.DB().QueryRow(`SELECT id,token_hash FROM admin_sessions WHERE admin_id=? AND device_id=? AND revoked_at IS NULL ORDER BY created_at DESC LIMIT 1`, adminID, deviceID).Scan(&sessionID, &storedHash)
	token := strings.TrimSpace(in.Token)
	switch {
	case err == nil && token != "" && secureTokenEqual(storedHash, tokenHash(token)):
		_, _ = a.store.DB().Exec(`UPDATE admin_sessions SET expires_at=?,last_seen_at=? WHERE id=?`, expires, nowText, sessionID)
	case err == nil:
		// Same session row (so the offline key stays), new token.
		fresh, terr := newToken()
		if terr != nil {
			writeError(w, http.StatusInternalServerError, "failed to create admin session")
			return
		}
		token = fresh
		_, _ = a.store.DB().Exec(`UPDATE admin_sessions SET token_hash=?,expires_at=?,last_seen_at=? WHERE id=?`, tokenHash(token), expires, nowText, sessionID)
	case err == sql.ErrNoRows:
		fresh, terr := newToken()
		if terr != nil {
			writeError(w, http.StatusInternalServerError, "failed to create admin session")
			return
		}
		token = fresh
		sessionID = uuid.NewString()
		if _, err := a.store.DB().Exec(`INSERT INTO admin_sessions(id,admin_id,token_hash,created_at,last_seen_at,expires_at,device_id) VALUES(?,?,?,?,?,?,?)`,
			sessionID, adminID, tokenHash(token), nowText, nowText, expires, deviceID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create admin session")
			return
		}
		_, _ = a.store.DB().Exec(`UPDATE admin_accounts SET last_login_at=?,updated_at=? WHERE id=?`, nowText, nowText, adminID)
		a.adminAudit(adminID, "admin_device_session", deviceID, "app admin session")
	default:
		writeError(w, http.StatusInternalServerError, "failed to read admin session")
		return
	}
	var acct AdminAccount
	_ = a.store.DB().QueryRow(`SELECT aa.id,COALESCE(u.username,aa.username),aa.role,aa.status,aa.created_at,COALESCE(aa.user_id,'') FROM admin_accounts aa LEFT JOIN users u ON u.id=aa.user_id WHERE aa.id=?`, adminID).
		Scan(&acct.ID, &acct.Username, &acct.Role, &acct.Status, &acct.CreatedAt, &acct.UserID)
	// The card must say the current role and "on".
	card, blob := a.currentBadge(userID)
	if card == nil || !card.On || card.Role != role {
		if b, err := a.serverAdminBadge(userID, role, true, "server"); err == nil {
			blob = &b
		}
	}
	out := map[string]any{"admin": acct, "token": token, "expires_at": expires}
	if blob != nil {
		out["card"] = *blob
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminBadges: every admin card and every "key ended" note (device auth).
func (a *API) AdminBadges(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authorizeDevice(r); !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	badges := []signedBlob{}
	if rows, err := a.store.DB().Query(`SELECT body,sig FROM admin_badges ORDER BY updated_at DESC LIMIT 500`); err == nil {
		for rows.Next() {
			var b signedBlob
			if rows.Scan(&b.Body, &b.Sig) == nil {
				badges = append(badges, b)
			}
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{"badges": badges, "ended": a.certEndedNotes()})
}
