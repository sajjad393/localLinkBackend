package httpapi

import (
	"encoding/json"
	"strings"
	"time"
)

// Bundle 307 (TODO_307 Part A): Get chat requests close by themselves.
//
//   A1  phones upload their signed "seen" notes ("I have no copy of this
//       chat" = why empty) in the normal admin-actions sync. A relay phone
//       uploads the notes it carried too. Each note is checked with the
//       phone's own signing key (or the phone itself is the uploader).
//   A2  when every target phone of a pending get_chat / get_file said
//       "empty", the request becomes cancelled + empty ("No messages") and
//       the signed stop note travels like any other cancel.
//   A4  an offline request's chat (kind + people / group) comes with its
//       label, so the web can list it and fetch its parts.
//   A5  the admin phone that made an offline request uploads its final
//       state once (done / cancelled / empty).
//
// Two small tables, made on first use (no migration, like duplicate_waits).

const (
	adminSeenType307    = "admin_act_seen"
	maxSeenPerSync307   = 500
	maxClosedPerSync307 = 200
)

type adminSeenNote307 struct {
	T      string `json:"t"`
	ID     string `json:"id"`
	Device string `json:"device"`
	User   string `json:"user"`
	Why    string `json:"why"`
	At     string `json:"at"`
}

// adminClosed307 is one final state sent by the admin phone that made it.
type adminClosed307 struct {
	ID    string `json:"id"`
	State string `json:"state"` // done | cancelled
	Empty bool   `json:"empty"`
}

func (a *API) ensureChat307() {
	_, _ = a.store.DB().Exec(`CREATE TABLE IF NOT EXISTS admin_action_seen_307(
		action_id TEXT NOT NULL,
		device_id TEXT NOT NULL,
		why TEXT NOT NULL,
		at TEXT NOT NULL DEFAULT '',
		via TEXT NOT NULL DEFAULT '',
		received_at TEXT NOT NULL,
		PRIMARY KEY(action_id, device_id, why))`)
	_, _ = a.store.DB().Exec(`CREATE TABLE IF NOT EXISTS admin_chat_empty_307(
		action_id TEXT PRIMARY KEY,
		at TEXT NOT NULL)`)
}

// actionTargets307 lists the target phones of an action ('[]' = not known yet).
func (a *API) actionTargets307(id string) (action, state string, devices []string, ok bool) {
	var tj string
	if a.store.DB().QueryRow(`SELECT action,state,targets_json FROM admin_actions WHERE id=?`, id).Scan(&action, &state, &tj) != nil {
		return "", "", nil, false
	}
	targets := []adminActionTarget{}
	_ = json.Unmarshal([]byte(tj), &targets)
	for _, t := range targets {
		if t.Device != "" {
			devices = append(devices, t.Device)
		}
	}
	return action, state, devices, true
}

// applySeenNote307 saves one signed seen note. It returns true when it was new.
func (a *API) applySeenNote307(blob signedBlob, uploader string) bool {
	if len(blob.Body) > 4<<10 {
		return false
	}
	var n adminSeenNote307
	if json.Unmarshal([]byte(blob.Body), &n) != nil || n.T != adminSeenType307 ||
		strings.TrimSpace(n.ID) == "" || strings.TrimSpace(n.Device) == "" {
		return false
	}
	if n.Why != "empty" && n.Why != "no_trust" {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, n.At); err != nil {
		return false
	}
	via := "server"
	if uploader != n.Device {
		var key string
		_ = a.store.DB().QueryRow(`SELECT signing_key FROM device_signing_keys WHERE device_id=?`, n.Device).Scan(&key)
		if key == "" || !verifyWithKey(key, blob) {
			return false // never trust a carried note we can't check
		}
		via = "carried"
	}
	action, _, targets, ok := a.actionTargets307(n.ID)
	if !ok || (action != "get_chat" && action != "get_file") {
		return false
	}
	if len(targets) > 0 {
		in := false
		for _, d := range targets {
			if d == n.Device {
				in = true
				break
			}
		}
		if !in {
			return false // only a target phone counts
		}
	}
	a.ensureChat307()
	res, err := a.store.DB().Exec(`INSERT OR IGNORE INTO admin_action_seen_307(action_id,device_id,why,at,via,received_at) VALUES(?,?,?,?,?,?)`,
		n.ID, n.Device, n.Why, n.At, via, nowStamp())
	if err != nil || resAffected(res) == 0 {
		return false
	}
	if n.Why == "empty" {
		a.closeIfAllEmpty307(n.ID)
	}
	return true
}

// closeIfAllEmpty307: every target phone has no copy -> cancelled + empty.
func (a *API) closeIfAllEmpty307(id string) bool {
	action, state, targets, ok := a.actionTargets307(id)
	if !ok || state != "pending" || len(targets) == 0 || (action != "get_chat" && action != "get_file") {
		return false
	}
	a.ensureChat307()
	for _, d := range targets {
		var n int
		_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_action_seen_307 WHERE action_id=? AND device_id=? AND why='empty'`, id, d).Scan(&n)
		if n == 0 {
			return false
		}
	}
	_, _ = a.store.DB().Exec(`INSERT OR IGNORE INTO admin_chat_empty_307(action_id,at) VALUES(?,?)`, id, nowStamp())
	if a.cancelAdminAction(id, "server", "", false) {
		a.adminAudit("", "admin_chat_empty", id, "every phone said it has no copy of this chat")
	}
	return true
}

// chatEmpty307 says if a request closed as "No messages".
func (a *API) chatEmpty307(id string) bool {
	a.ensureChat307()
	var n int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_chat_empty_307 WHERE action_id=?`, id).Scan(&n)
	return n > 0
}

// applyClosed307: the admin phone that made an offline request tells its end.
func (a *API) applyClosed307(fromDevice string, c adminClosed307) bool {
	id := strings.TrimSpace(c.ID)
	if id == "" || (c.State != "done" && c.State != "cancelled") {
		return false
	}
	var body, signer, action, state string
	if a.store.DB().QueryRow(`SELECT body,signer,action,state FROM admin_actions WHERE id=?`, id).Scan(&body, &signer, &action, &state) != nil ||
		signer != "admin" || !chatActions[action] {
		return false
	}
	var card adminActionBody
	if json.Unmarshal([]byte(body), &card) != nil || card.Cert == nil {
		return false
	}
	var cert adminCertBody
	if json.Unmarshal([]byte(card.Cert.Body), &cert) != nil || cert.DeviceID != fromDevice {
		return false // only the phone that made it
	}
	if c.State == "cancelled" && c.Empty {
		a.ensureChat307()
		_, _ = a.store.DB().Exec(`INSERT OR IGNORE INTO admin_chat_empty_307(action_id,at) VALUES(?,?)`, id, nowStamp())
	}
	if state != "pending" {
		return false
	}
	if c.State == "cancelled" {
		return a.cancelAdminAction(id, cert.Username, cert.AdminID, false)
	}
	res, err := a.store.DB().Exec(`UPDATE admin_actions SET state='done',finished_at=? WHERE id=? AND state='pending'`, nowStamp(), id)
	if err != nil || resAffected(res) == 0 {
		return false
	}
	a.signActionState(id)
	a.adminAudit(cert.AdminID, "admin_action_finished", id, "done on the admin phone")
	a.hintAdminActions()
	return true
}

// rememberLabelChat307 (A4): the chat of an offline request, from its label.
func (a *API) rememberLabelChat307(id string, detail map[string]any) {
	if detail == nil {
		return
	}
	raw, err := json.Marshal(detail["chat"])
	if err != nil {
		return
	}
	var ref chatRef
	if json.Unmarshal(raw, &ref) != nil || (ref.Kind != "direct" && ref.Kind != "group") {
		return
	}
	if len(ref.Users) > 10 {
		ref.Users = ref.Users[:10]
	}
	if len(ref.Devices) > 50 {
		ref.Devices = ref.Devices[:50]
	}
	day := func(k string) string {
		s, _ := detail[k].(string)
		if d, ok := dayOf(s); ok {
			return d
		}
		return ""
	}
	a.rememberChat(id, ref, day("from"), day("to"))
	if f, _ := detail["file_id"].(string); strings.TrimSpace(f) != "" && len(f) <= 200 {
		_, _ = a.store.DB().Exec(`UPDATE admin_chat_request_meta SET file_id=? WHERE action_id=?`, f, id)
	}
}

// reachedPhones307: target phones that sent anything back (a receipt, a
// "seen" note or answer parts). The live card's "1 of 2 phones".
func (a *API) reachedPhones307(id string, targets []adminActionTarget) int {
	// No table yet (no seen note ever) = the query fails and is skipped.
	got := map[string]bool{}
	for _, q := range []string{
		`SELECT device_id FROM admin_action_receipts WHERE action_id=?`,
		`SELECT device_id FROM admin_action_seen_307 WHERE action_id=?`,
	} {
		rows, err := a.store.DB().Query(q, id)
		if err != nil {
			continue
		}
		for rows.Next() {
			var d string
			if rows.Scan(&d) == nil && d != "" {
				got[d] = true
			}
		}
		rows.Close()
	}
	if _, device, _ := a.bestAnswer(id); device != "" {
		got[device] = true
	}
	if len(targets) == 0 {
		return len(got)
	}
	n := 0
	for _, t := range targets {
		if got[t.Device] {
			n++
		}
	}
	return n
}
