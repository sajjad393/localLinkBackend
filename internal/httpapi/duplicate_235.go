package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bundle 235: one phone = one account.
//
// A new account made on a phone that already belongs to another account is
// refused (409 DEVICE_TAKEN, as before) and the answer now also says
// "duplicate": the person must prove the old account (recovery code or old
// password) in the app. Every refusal is kept in duplicate_waits so the web
// admin sees "Waiting for proof". Only the admin can give the phone to a new
// person ("New owner"). Nothing is merged.

// ensureDuplicateTable235 makes the small table on first use (no migration).
func (a *API) ensureDuplicateTable235() {
	_, _ = a.store.DB().Exec(`CREATE TABLE IF NOT EXISTS duplicate_waits(
		id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		device_name TEXT NOT NULL DEFAULT '',
		hardware_fp TEXT NOT NULL DEFAULT '',
		old_user_id TEXT NOT NULL,
		new_username TEXT NOT NULL,
		new_name TEXT NOT NULL DEFAULT '',
		via TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'waiting',
		tries INTEGER NOT NULL DEFAULT 1,
		first_seen TEXT NOT NULL,
		last_seen TEXT NOT NULL,
		done_at TEXT NOT NULL DEFAULT '',
		done_by TEXT NOT NULL DEFAULT '')`)
	_, _ = a.store.DB().Exec(`CREATE INDEX IF NOT EXISTS idx_duplicate_waits_device ON duplicate_waits(device_id,status)`)
}

type duplicateOwner235 struct {
	DeviceID   string
	DeviceName string
	UserID     string
	Username   string
	Name       string
}

// findDuplicateOwner235 returns the account that already owns this very phone
// (same hardware id, active phone, account not deleted) when it is not the
// account being made. nil = no duplicate (unknown phone, same account, or no
// hardware id sent).
func (a *API) findDuplicateOwner235(deviceID, hardwareFP, username string) *duplicateOwner235 {
	hardwareFP = strings.TrimSpace(hardwareFP)
	if hardwareFP == "" {
		return nil
	}
	var o duplicateOwner235
	// The phone row with this id first, else any active phone with the same
	// hardware id (a reinstall that made a new phone id).
	err := a.store.DB().QueryRow(`SELECT d.id,COALESCE(d.name,''),u.id,u.username,COALESCE(p.display_name,'')
		FROM devices d JOIN users u ON u.id=d.user_id LEFT JOIN profiles p ON p.user_id=u.id
		WHERE COALESCE(d.hardware_fp,'')=? AND COALESCE(d.status,'active')='active'
		AND COALESCE(u.status,'active') IN ('active','suspended')
		ORDER BY CASE WHEN d.id=? THEN 0 ELSE 1 END, d.last_seen_at DESC LIMIT 1`, hardwareFP, deviceID).
		Scan(&o.DeviceID, &o.DeviceName, &o.UserID, &o.Username, &o.Name)
	if err != nil {
		return nil
	}
	if strings.EqualFold(o.Username, strings.TrimSpace(username)) {
		return nil // the same account again (re-login), not a duplicate
	}
	// Bundle 276: never "My Android Phone (xxxx)" as a person or a phone.
	o.Name = personName276(o.Name, o.Username)
	o.DeviceName = phoneLabel276(o.DeviceName, o.Name)
	return &o
}

// noteDuplicateWait235 keeps one "waiting" line per phone + new name.
func (a *API) noteDuplicateWait235(o *duplicateOwner235, deviceID, hardwareFP, newUsername, newName, via string) {
	if o == nil || strings.TrimSpace(newUsername) == "" {
		return
	}
	a.ensureDuplicateTable235()
	now := nowStamp()
	db := a.store.DB()
	var id string
	err := db.QueryRow(`SELECT id FROM duplicate_waits WHERE device_id=? AND new_username=? AND status='waiting'`, o.DeviceID, newUsername).Scan(&id)
	if err == nil {
		_, _ = db.Exec(`UPDATE duplicate_waits SET tries=tries+1,last_seen=?,old_user_id=? WHERE id=?`, now, o.UserID, id)
	} else {
		name := strings.TrimSpace(o.DeviceName)
		if name == "" {
			name = deviceID
		}
		_, _ = db.Exec(`INSERT INTO duplicate_waits(id,device_id,device_name,hardware_fp,old_user_id,new_username,new_name,via,status,tries,first_seen,last_seen) VALUES(?,?,?,?,?,?,?,?,'waiting',1,?,?)`,
			uuid.NewString(), o.DeviceID, name, hardwareFP, o.UserID, newUsername, newName, via, now, now)
	}
	a.audit("duplicate_refused", o.UserID, "device="+o.DeviceID+" new="+newUsername+" via="+via)
}

// writeDuplicate235 is the register answer: the same DEVICE_TAKEN as before
// plus who owns the phone.
func writeDuplicate235(w http.ResponseWriter, o *duplicateOwner235) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"ok":            false,
		"error":         "this phone already has an account",
		"error_code":    "DEVICE_TAKEN",
		"owner":         o.Username,
		"duplicate":     true,
		"owner_user_id": o.UserID,
		"owner_name":    o.Name,
	})
}

// clearDuplicateWaits235: the old account proved itself on that phone.
func (a *API) clearDuplicateWaits235(deviceID, by string) {
	if strings.TrimSpace(deviceID) == "" {
		return
	}
	a.ensureDuplicateTable235()
	_, _ = a.store.DB().Exec(`UPDATE duplicate_waits SET status='proved',done_at=?,done_by=? WHERE device_id=? AND status='waiting'`, nowStamp(), by, deviceID)
}

type duplicateWait235 struct {
	ID          string `json:"id"`
	DeviceID    string `json:"device_id"`
	DeviceName  string `json:"device_name"`
	OldUserID   string `json:"old_user_id"`
	OldUsername string `json:"old_username"`
	OldName     string `json:"old_name"`
	NewUsername string `json:"new_username"`
	NewName     string `json:"new_name"`
	Via         string `json:"via"`
	Tries       int    `json:"tries"`
	FirstSeen   string `json:"first_seen"`
	LastSeen    string `json:"last_seen"`
}

// AdminDuplicates235 is GET /api/v1/admin/duplicates: the phones waiting for
// proof (newest first).
func (a *API) AdminDuplicates235(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	a.ensureDuplicateTable235()
	rows, err := a.store.DB().Query(`SELECT w.id,w.device_id,w.device_name,w.old_user_id,COALESCE(u.username,''),COALESCE(p.display_name,''),w.new_username,w.new_name,w.via,w.tries,w.first_seen,w.last_seen
		FROM duplicate_waits w LEFT JOIN users u ON u.id=w.old_user_id LEFT JOIN profiles p ON p.user_id=w.old_user_id
		WHERE w.status='waiting' ORDER BY w.last_seen DESC LIMIT 200`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read list")
		return
	}
	out := []duplicateWait235{}
	for rows.Next() {
		var d duplicateWait235
		if rows.Scan(&d.ID, &d.DeviceID, &d.DeviceName, &d.OldUserID, &d.OldUsername, &d.OldName, &d.NewUsername, &d.NewName, &d.Via, &d.Tries, &d.FirstSeen, &d.LastSeen) == nil {
			d.OldName = personName276(d.OldName, d.OldUsername) // Bundle 276
			d.DeviceName = phoneLabel276(d.DeviceName, firstNonEmpty216(d.OldName, d.OldUsername))
			out = append(out, d)
		}
	}
	rows.Close()
	writeJSON(w, http.StatusOK, map[string]any{"duplicates": out})
}

func (a *API) readDuplicateWait235(id string) (duplicateWait235, bool) {
	var d duplicateWait235
	err := a.store.DB().QueryRow(`SELECT id,device_id,device_name,old_user_id,new_username,new_name FROM duplicate_waits WHERE id=? AND status='waiting'`, id).
		Scan(&d.ID, &d.DeviceID, &d.DeviceName, &d.OldUserID, &d.NewUsername, &d.NewName)
	return d, err == nil
}

// AdminDuplicateNewOwner235 is POST /api/v1/admin/duplicates/new-owner
// {"id","confirm"}: the admin typed the new @name. The phone leaves the old
// account (the old account keeps its name, password, chats and other
// phones); the new account can then join from it. No undo.
func (a *API) AdminDuplicateNewOwner235(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_users")
	if !ok {
		return
	}
	var in struct {
		ID      string `json:"id"`
		Confirm string `json:"confirm"`
	}
	if err := decodeJSON(r, &in, 4<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a.ensureDuplicateTable235()
	d, found := a.readDuplicateWait235(strings.TrimSpace(in.ID))
	if !found {
		writeError(w, http.StatusNotFound, "not waiting")
		return
	}
	typed := strings.TrimPrefix(strings.TrimSpace(in.Confirm), "@")
	if !strings.EqualFold(typed, d.NewUsername) {
		writeErrorCode(w, http.StatusBadRequest, "CONFIRM_MISMATCH", "type the new name to confirm")
		return
	}
	now := nowStamp()
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start")
		return
	}
	freed := freeDevice235(tx, d.DeviceID, d.OldUserID)
	if !freed {
		_ = tx.Rollback()
		writeErrorCode(w, http.StatusConflict, "DEVICE_BUSY", "this phone could not be freed (it still holds group history); remove the phone from the old account first")
		return
	}
	if _, err := tx.Exec(`UPDATE duplicate_waits SET status='freed',done_at=?,done_by=? WHERE device_id=? AND status='waiting'`, now, admin.ID, d.DeviceID); err != nil {
		_ = tx.Rollback()
		writeError(w, http.StatusInternalServerError, "failed to save")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save")
		return
	}
	a.endDeviceAdminSessions(d.DeviceID)
	a.hub.CloseDevice(d.DeviceID)
	a.presenceMu.Lock()
	delete(a.presence, d.DeviceID)
	a.presenceMu.Unlock()
	a.adminAudit(admin.ID, "duplicate_new_owner", d.DeviceID, "old="+d.OldUserID+" new="+d.NewUsername)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "freed": true})
}

// AdminDuplicateDismiss235 is POST /api/v1/admin/duplicates/dismiss {"id"}:
// hides the line only. The new account stays blocked.
func (a *API) AdminDuplicateDismiss235(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_users")
	if !ok {
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &in, 4<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a.ensureDuplicateTable235()
	res, err := a.store.DB().Exec(`UPDATE duplicate_waits SET status='dismissed',done_at=?,done_by=? WHERE id=? AND status='waiting'`, nowStamp(), admin.ID, strings.TrimSpace(in.ID))
	if err != nil || resAffected(res) == 0 {
		writeError(w, http.StatusNotFound, "not waiting")
		return
	}
	a.adminAudit(admin.ID, "duplicate_dismiss", strings.TrimSpace(in.ID), "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// freeDevice235 deletes the phone row of the old account inside tx (its
// sessions, keys and seats go by ON DELETE CASCADE; rows with no cascade are
// cleared here). false = refused by the database (e.g. group history) and
// nothing changed.
func freeDevice235(tx *sql.Tx, deviceID, oldUserID string) bool {
	var owner sql.NullString
	if err := tx.QueryRow(`SELECT user_id FROM devices WHERE id=?`, deviceID).Scan(&owner); err != nil {
		return err == sql.ErrNoRows // already gone = free
	}
	if owner.String != oldUserID {
		return false // the phone changed hands meanwhile
	}
	if _, err := tx.Exec(`SAVEPOINT free235`); err != nil {
		return false
	}
	undo := func() bool {
		_, _ = tx.Exec(`ROLLBACK TO free235`)
		_, _ = tx.Exec(`RELEASE free235`)
		return false
	}
	for _, q := range []string{
		`DELETE FROM sync_events WHERE device_id=?`,
		`DELETE FROM device_signing_keys WHERE device_id=?`,
		`DELETE FROM device_builds WHERE device_id=?`,
		`DELETE FROM group_join_requests WHERE device_id=?`,
		`DELETE FROM seen_offline_accounts WHERE device_id=?`,
		`DELETE FROM messages WHERE recipient_id=? AND delivered_at IS NULL`,
	} {
		if _, err := tx.Exec(q, deviceID); err != nil && !strings.Contains(err.Error(), "no such table") && !strings.Contains(err.Error(), "no such column") {
			return undo()
		}
	}
	if _, err := tx.Exec(`DELETE FROM devices WHERE id=?`, deviceID); err != nil {
		return undo()
	}
	if _, err := tx.Exec(`RELEASE free235`); err != nil {
		return undo()
	}
	return true
}

// ReturnNote235 is POST /api/v1/account/return-note (signed-in phone): after
// the old account proved itself on this phone with the server, the server
// signs a short note with its trust key. Friends' phones that pinned the
// server key take the new phone keys from it, even with no server later.
func (a *API) ReturnNote235(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	userID, _ := a.deviceUser(deviceID)
	if userID == "" {
		writeError(w, http.StatusForbidden, "account required")
		return
	}
	var in struct {
		SigningKey  string `json:"signing_key"`
		IdentityKey string `json:"identity_key"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.SigningKey, in.IdentityKey = strings.TrimSpace(in.SigningKey), strings.TrimSpace(in.IdentityKey)
	if in.SigningKey == "" || len(in.SigningKey) > 200 || len(in.IdentityKey) > 200 {
		writeError(w, http.StatusBadRequest, "keys missing")
		return
	}
	var username, name string
	_ = a.store.DB().QueryRow(`SELECT u.username,COALESCE(p.display_name,'') FROM users u LEFT JOIN profiles p ON p.user_id=u.id WHERE u.id=?`, userID).Scan(&username, &name)
	body := map[string]any{
		"v":            1,
		"kind":         "account_return_235",
		"user_id":      userID,
		"username":     username,
		"name":         name,
		"device_id":    deviceID,
		"signing_key":  in.SigningKey,
		"identity_key": in.IdentityKey,
		"at":           time.Now().UTC().UnixMilli(),
		"via":          "server",
	}
	raw, _ := json.Marshal(body)
	signed, err := a.serverSign(string(raw))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "server key not ready")
		return
	}
	a.clearDuplicateWaits235(deviceID, userID)
	a.audit("account_return_note", userID, "device="+deviceID)
	writeJSON(w, http.StatusOK, signed)
}
