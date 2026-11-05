package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bundle 212: admin block / unblock records.
//
// A record is a signed body {v, kind:"admin_block", id, target_user_id,
// target_devices, target_name, action, seq, at, by, signer, cert}. It is
// signed either by the server key (signer "server", web panel) or by an
// owner admin's phone key (signer "admin") whose certificate the server
// signed. Phones carry records with no internet; any phone that gets online
// sends them here. The newest record per user wins: higher seq, then later
// time, then the larger id.

type adminBlockBody struct {
	V             int         `json:"v"`
	Kind          string      `json:"kind"`
	ID            string      `json:"id"`
	TargetUserID  string      `json:"target_user_id"`
	TargetDevices []string    `json:"target_devices"`
	TargetName    string      `json:"target_name"`
	Action        string      `json:"action"`
	Seq           int64       `json:"seq"`
	At            string      `json:"at"`
	By            string      `json:"by"`
	Signer        string      `json:"signer"`
	Cert          *signedBlob `json:"cert,omitempty"`
}

const maxAdminBlockRecords = 200

// checkAdminBlock returns the parsed body when the record is genuine.
func (a *API) checkAdminBlock(blob signedBlob) (adminBlockBody, string) {
	var rec adminBlockBody
	if len(blob.Body) > 16<<10 {
		return rec, "too_big"
	}
	if err := json.Unmarshal([]byte(blob.Body), &rec); err != nil {
		return rec, "bad_json"
	}
	if rec.V != 1 || rec.Kind != "admin_block" || strings.TrimSpace(rec.ID) == "" || strings.TrimSpace(rec.TargetUserID) == "" || rec.Seq < 1 {
		return rec, "bad_fields"
	}
	if rec.Action != "block" && rec.Action != "unblock" {
		return rec, "bad_action"
	}
	if _, err := time.Parse(time.RFC3339Nano, rec.At); err != nil {
		return rec, "bad_time"
	}
	switch rec.Signer {
	case "server":
		if !a.serverVerify(blob) {
			return rec, "bad_server_sig"
		}
		return rec, ""
	case "admin":
		if rec.Cert == nil || !a.serverVerify(*rec.Cert) {
			return rec, "bad_cert"
		}
		var cert adminCertBody
		if err := json.Unmarshal([]byte(rec.Cert.Body), &cert); err != nil || cert.Kind != "admin_cert" {
			return rec, "bad_cert"
		}
		var ended int
		_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_cert_ended WHERE cert_id=?`, cert.CertID).Scan(&ended)
		if ended > 0 {
			return rec, "cert_ended"
		}
		if !adminPermission(cert.Role, "manage_users") {
			return rec, "not_owner"
		}
		if !verifyWithKey(cert.SigningKey, blob) {
			return rec, "bad_sig"
		}
		// The admin can't block their own account (the cert phone's user).
		var ownUser string
		_ = a.store.DB().QueryRow(`SELECT COALESCE(user_id,'') FROM devices WHERE id=?`, cert.DeviceID).Scan(&ownUser)
		if ownUser != "" && ownUser == rec.TargetUserID {
			return rec, "self"
		}
		for _, d := range rec.TargetDevices {
			if d == cert.DeviceID {
				return rec, "self"
			}
		}
		return rec, ""
	default:
		return rec, "bad_signer"
	}
}

// adminBlockNewer reports whether a should replace b.
func adminBlockNewer(aSeq int64, aAt, aID string, bSeq int64, bAt, bID string) bool {
	if aSeq != bSeq {
		return aSeq > bSeq
	}
	ta, _ := time.Parse(time.RFC3339Nano, aAt)
	tb, _ := time.Parse(time.RFC3339Nano, bAt)
	if !ta.Equal(tb) {
		return ta.After(tb)
	}
	return aID > bID
}

// applyAdminBlock saves the record when it is newer and sets the user status.
// It never revokes sessions, so an unblock brings the phone back with no new
// sign-in. A disabled user stays disabled.
func (a *API) applyAdminBlock(rec adminBlockBody, blob signedBlob) (bool, error) {
	tx, err := a.store.DB().Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var curSeq int64
	var curAt, curID string
	err = tx.QueryRow(`SELECT seq,at,record_id FROM admin_blocks WHERE target_user_id=?`, rec.TargetUserID).Scan(&curSeq, &curAt, &curID)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil && !adminBlockNewer(rec.Seq, rec.At, rec.ID, curSeq, curAt, curID) {
		return false, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT INTO admin_blocks(target_user_id,record_id,action,seq,at,body,sig,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(target_user_id) DO UPDATE SET record_id=excluded.record_id,action=excluded.action,seq=excluded.seq,at=excluded.at,body=excluded.body,sig=excluded.sig,updated_at=excluded.updated_at`,
		rec.TargetUserID, rec.ID, rec.Action, rec.Seq, rec.At, blob.Body, blob.Sig, now); err != nil {
		return false, err
	}
	status := "suspended"
	if rec.Action == "unblock" {
		status = "active"
	}
	if _, err := tx.Exec(`UPDATE users SET status=?,updated_at=? WHERE id=? AND status IN ('active','suspended')`, status, now, rec.TargetUserID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	a.adminAudit(rec.By, "admin_block_"+rec.Action, rec.TargetUserID, "signer="+rec.Signer+" record="+rec.ID)
	return true, nil
}

func (a *API) currentAdminBlocks() []signedBlob {
	rows, err := a.store.DB().Query(`SELECT body,sig FROM admin_blocks ORDER BY updated_at DESC LIMIT ?`, maxAdminBlockRecords)
	if err != nil {
		return []signedBlob{}
	}
	defer rows.Close()
	out := []signedBlob{}
	for rows.Next() {
		var b signedBlob
		if rows.Scan(&b.Body, &b.Sig) == nil {
			out = append(out, b)
		}
	}
	return out
}

// serverAdminBlock makes a server-signed record (web panel suspend /
// unsuspend) with seq = current + 1 and applies it.
func (a *API) serverAdminBlock(userID, action, by string) error {
	var curSeq int64
	_ = a.store.DB().QueryRow(`SELECT seq FROM admin_blocks WHERE target_user_id=?`, userID).Scan(&curSeq)
	var name string
	_ = a.store.DB().QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&name)
	devices := []string{}
	if rows, err := a.store.DB().Query(`SELECT id FROM devices WHERE user_id=? ORDER BY id`, userID); err == nil {
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				devices = append(devices, id)
			}
		}
		rows.Close()
	}
	sort.Strings(devices)
	rec := adminBlockBody{V: 1, Kind: "admin_block", ID: uuid.NewString(), TargetUserID: userID, TargetDevices: devices,
		TargetName: name, Action: action, Seq: curSeq + 1, At: time.Now().UTC().Format(time.RFC3339Nano), By: by, Signer: "server"}
	raw, _ := json.Marshal(rec)
	blob, err := a.serverSign(string(raw))
	if err != nil {
		return err
	}
	_, err = a.applyAdminBlock(rec, blob)
	return err
}

// AdminBlocksSync: any signed-in phone sends the records it carries and gets
// every current record and every "key ended" note back.
func (a *API) AdminBlocksSync(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authorizeDevice(r); !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var in struct {
		Records []signedBlob `json:"records"`
	}
	if err := decodeJSON(r, &in, 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(in.Records) > maxAdminBlockRecords {
		in.Records = in.Records[:maxAdminBlockRecords]
	}
	applied, rejected := 0, 0
	for _, blob := range in.Records {
		rec, reason := a.checkAdminBlock(blob)
		if reason != "" {
			rejected++
			continue
		}
		if ok, err := a.applyAdminBlock(rec, blob); err == nil && ok {
			applied++
			a.adminActionForBlockRecord(rec, blob) // Bundle 220
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"records":  a.currentAdminBlocks(),
		"ended":    a.certEndedNotes(),
		"applied":  applied,
		"rejected": rejected,
	})
}

// AdminBlocksMine: the blocked phone can't pass device auth (its user is
// suspended), so it checks its own device id + token without the status
// check and gets only its own record back.
func (a *API) AdminBlocksMine(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.Header.Get("X-Device-ID"))
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if id == "" || token == "" {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var storedHash, userID, deviceStatus string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(token_hash,''),COALESCE(user_id,''),COALESCE(status,'active') FROM devices WHERE id=?`, id).Scan(&storedHash, &userID, &deviceStatus); err != nil ||
		storedHash == "" || !secureTokenEqual(storedHash, tokenHash(token)) || deviceStatus != "active" || userID == "" {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var b signedBlob
	err := a.store.DB().QueryRow(`SELECT body,sig FROM admin_blocks WHERE target_user_id=?`, userID).Scan(&b.Body, &b.Sig)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"record": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"record": b})
}

// carryOfflineBlock218 (Bundle 218): a block made while the person was a
// phone-only account (record on offline_x, or on her phone id) follows her
// to the real account when she joins the server.
func (a *API) carryOfflineBlock218(userID, offlineID, deviceID string) {
	rows, err := a.store.DB().Query(`SELECT target_user_id,action,seq,at,record_id,body FROM admin_blocks LIMIT ?`, maxAdminBlockRecords)
	if err != nil {
		return
	}
	type row struct {
		target, action, at, id, body string
		seq                          int64
	}
	var all []row
	for rows.Next() {
		var x row
		if rows.Scan(&x.target, &x.action, &x.seq, &x.at, &x.id, &x.body) == nil {
			all = append(all, x)
		}
	}
	rows.Close()
	var best *row
	var mine *row
	for i := range all {
		x := &all[i]
		if x.target == userID {
			mine = x
			continue
		}
		hit := x.target == offlineID
		if !hit && deviceID != "" {
			var body adminBlockBody
			if json.Unmarshal([]byte(x.body), &body) == nil {
				for _, d := range body.TargetDevices {
					if d == deviceID {
						hit = true
						break
					}
				}
			}
		}
		if hit && (best == nil || adminBlockNewer(x.seq, x.at, x.id, best.seq, best.at, best.id)) {
			best = x
		}
	}
	if best == nil || best.action != "block" {
		return
	}
	if mine != nil {
		mt, _ := time.Parse(time.RFC3339Nano, mine.at)
		bt, _ := time.Parse(time.RFC3339Nano, best.at)
		if mt.After(bt) {
			return
		}
	}
	if err := a.serverAdminBlock(userID, "block", "auto: blocked while offline"); err != nil {
		return
	}
	a.adminAudit("server", "admin_block_carry", userID, "from="+offlineID)
}
