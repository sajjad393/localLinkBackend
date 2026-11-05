package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bundle 220: every admin action goes to all phones.
//
// An action is a signed card {v, kind:"admin_action", id, action, seq, at,
// by, signer, cert?, need, to:[{k, box}], inner?, cancels?, parent?}. The
// card is the same on every phone; only the target phone can open its box
// (admin_actions_box.go). Phones pass cards on over every link (server,
// Wi-Fi, Wi-Fi Direct, mesh), friends or not. The target does the action and
// sends a signed "done" receipt back; a receipt or a server-signed state note
// is the "stop" that tells the other phones not to carry the card any more.
// There is no time limit: an action stays Pending until it is done or the
// admin cancels it.

const (
	adminActionAppBuild      = 131 // phones older than this can't open cards
	maxAdminActionCardBytes  = 16 << 10
	maxLiveAdminActions      = 1000
	maxAdminActionTombstones = 5000
	maxPendingPerAdmin       = 50
	adminActionSyncPage      = 100
)

type adminActionBox struct {
	K   string `json:"k"`
	Box string `json:"box"`
}

type adminActionBody struct {
	V        int              `json:"v"`
	Kind     string           `json:"kind"`
	ID       string           `json:"id"`
	Action   string           `json:"action"`
	Seq      int64            `json:"seq"`
	At       string           `json:"at"`
	By       string           `json:"by"`
	Signer   string           `json:"signer"`
	Cert     *signedBlob      `json:"cert,omitempty"`
	Need     int              `json:"need"`
	To       []adminActionBox `json:"to"`
	Inner    *signedBlob      `json:"inner,omitempty"`
	Cancels  string           `json:"cancels,omitempty"`
	Parent   string           `json:"parent,omitempty"`
	ReplyKey string           `json:"reply_key,omitempty"`
}

// adminActionBoxPlain is what is inside one box.
type adminActionBoxPlain struct {
	Device string         `json:"device"`
	User   string         `json:"user"`
	Detail map[string]any `json:"detail,omitempty"`
}

type adminActionDoneBody struct {
	V        int    `json:"v"`
	Kind     string `json:"kind"`
	ActionID string `json:"action_id"`
	Device   string `json:"device"`
	User     string `json:"user"`
	Result   string `json:"result"`
	At       string `json:"at"`
	Build    int64  `json:"build"`
	// How the card reached the target: server | wifi | wifi_direct | mesh.
	Via string `json:"via,omitempty"`
}

type adminActionStateBody struct {
	V     int    `json:"v"`
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	State string `json:"state"`
	Done  int    `json:"done"`
	Need  int    `json:"need"`
	At    string `json:"at"`
}

type adminActionTarget struct {
	Device string         `json:"device"`
	User   string         `json:"user"`
	Boxed  bool           `json:"boxed"`
	Detail map[string]any `json:"-"`
}

var adminActionNames = map[string]bool{
	"block": true, "unblock": true, "off": true, "active": true, "wifi_rule": true,
	"sign_out": true, "remove_phone": true, "admin_badge": true, "version_rule": true, "cancel": true,
}

// serverOnlyActions never leave the server (listed as "Done at once").
var serverOnlyActions = map[string]bool{"rename_phone": true}

// adminActionPermission is the admin role permission an action needs (the
// same permission the web page needs for it). "owner" means owner only.
func adminActionPermission(action string) string {
	switch action {
	case "block", "unblock", "off", "active":
		return "manage_users"
	case "wifi_rule":
		return "manage_network_policy"
	case "sign_out":
		return "manage_sessions"
	case "remove_phone":
		return "manage_devices"
	default:
		return "owner"
	}
}

func adminRoleCan(role, action string) bool {
	p := adminActionPermission(action)
	if p == "owner" {
		return role == "owner"
	}
	return adminPermission(role, p)
}

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// identityKeyFor is the phone's newest X25519 key (a removed phone's key is
// retired at the same moment, so retired keys are allowed as a fallback).
func (a *API) identityKeyFor(deviceID string) string {
	var key string
	_ = a.store.DB().QueryRow(`SELECT public_key FROM device_identity_keys WHERE device_id=?
		ORDER BY (retired_at IS NULL) DESC, key_version DESC LIMIT 1`, deviceID).Scan(&key)
	if key == "" {
		_ = a.store.DB().QueryRow(`SELECT COALESCE(identity_public_key,'') FROM devices WHERE id=?`, deviceID).Scan(&key)
	}
	return key
}

func (a *API) activePhonesOf(userID string) []string {
	out := []string{}
	rows, err := a.store.DB().Query(`SELECT id FROM devices WHERE user_id=? AND COALESCE(status,'active')='active' ORDER BY id`, userID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

func (a *API) usernameOf(userID string) string {
	var name string
	_ = a.store.DB().QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&name)
	return name
}

func (a *API) deviceUser(deviceID string) (string, string) {
	var userID, name string
	_ = a.store.DB().QueryRow(`SELECT COALESCE(user_id,''),COALESCE(name,'') FROM devices WHERE id=?`, deviceID).Scan(&userID, &name)
	return userID, name
}

type newAdminAction struct {
	Action  string
	By      string // admin username shown on phones
	AdminID string // for the audit
	Label   string
	Targets []adminActionTarget
	Inner   *signedBlob
	Cancels string
	Parent  string
	Detail  string // short words for the web list
	NoNeed  bool   // version rule: "Reached N", never Done
}

// makeServerAction signs and saves a server card and tells connected phones.
func (a *API) makeServerAction(p newAdminAction) (adminActionBody, error) {
	body := adminActionBody{V: 1, Kind: "admin_action", ID: uuid.NewString(), Action: p.Action,
		Seq: time.Now().UnixMilli(), At: nowStamp(), By: p.By, Signer: "server", To: []adminActionBox{},
		Inner: p.Inner, Cancels: p.Cancels, Parent: p.Parent}
	targets := make([]adminActionTarget, 0, len(p.Targets))
	for _, t := range p.Targets {
		key := a.identityKeyFor(t.Device)
		if key != "" {
			plain, _ := json.Marshal(adminActionBoxPlain{Device: t.Device, User: t.User, Detail: t.Detail})
			if box, err := lockBox(key, plain); err == nil {
				body.To = append(body.To, adminActionBox{K: strings.ReplaceAll(uuid.NewString(), "-", "")[:12], Box: box})
				t.Boxed = true
			}
		}
		targets = append(targets, t)
	}
	// The order of boxes must not tell which phone is which.
	sort.Slice(body.To, func(i, j int) bool { return body.To[i].K < body.To[j].K })
	if !p.NoNeed {
		body.Need = len(body.To)
	}
	raw, _ := json.Marshal(body)
	blob, err := a.serverSign(string(raw))
	if err != nil {
		return body, err
	}
	state := "pending"
	if !p.NoNeed && body.Need == 0 && p.Action != "version_rule" {
		state = "done" // nothing to reach (server-only action, or no phones)
	}
	if p.Action == "cancel" {
		state = "done"
	}
	tj, _ := json.Marshal(targets)
	_, err = a.store.DB().Exec(`INSERT INTO admin_actions(id,action,body,sig,signer,made_by,made_at,need,done,state,parent,target_label,targets_json,detail,offline,finished_at)
		VALUES(?,?,?,?,?,?,?,?,0,?,?,?,?,?,0,?)`,
		body.ID, body.Action, blob.Body, blob.Sig, "server", p.By, body.At, body.Need, state, p.Parent, p.Label, string(tj), p.Detail, nullIf(state != "done", body.At))
	if err != nil {
		return body, err
	}
	if state == "done" {
		a.signActionState(body.ID)
	}
	if p.Action == "version_rule" {
		// A newer rule replaces the older ones: they stop travelling.
		a.finishOlder("version_rule", body.ID)
	}
	if p.Action != "cancel" {
		a.adminAudit(p.AdminID, "admin_action_made", p.Label, "action="+p.Action+" card="+body.ID+" need="+strconv.Itoa(body.Need))
	}
	a.pruneAdminActions()
	a.hintAdminActions()
	return body, nil
}

func nullIf(isNull bool, v string) any {
	if isNull {
		return nil
	}
	return v
}

func (a *API) finishOlder(action, keepID string) {
	rows, err := a.store.DB().Query(`SELECT id FROM admin_actions WHERE action=? AND state='pending' AND id!=?`, action, keepID)
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_, _ = a.store.DB().Exec(`UPDATE admin_actions SET state='replaced',finished_at=? WHERE id=?`, nowStamp(), id)
		a.signActionState(id)
	}
}

// signActionState saves a server-signed note with the action's state; phones
// pass it on as the "stop" for done / cancelled / replaced cards.
func (a *API) signActionState(id string) {
	var state string
	var need, done int
	if a.store.DB().QueryRow(`SELECT state,need,done FROM admin_actions WHERE id=?`, id).Scan(&state, &need, &done) != nil {
		return
	}
	raw, _ := json.Marshal(adminActionStateBody{V: 1, Kind: "admin_action_state", ID: id, State: state, Done: done, Need: need, At: nowStamp()})
	blob, err := a.serverSign(string(raw))
	if err != nil {
		return
	}
	_, _ = a.store.DB().Exec(`UPDATE admin_actions SET state_body=?,state_sig=? WHERE id=?`, blob.Body, blob.Sig, id)
}

func (a *API) hintAdminActions() {
	if a.hub == nil {
		return
	}
	payload := mustJSON(WSMessage{Type: "admin_action"})
	for _, id := range a.hub.DeviceIDs() {
		a.hub.SendTo(id, payload)
	}
}

// pruneAdminActions keeps at most maxAdminActionTombstones finished cards.
func (a *API) pruneAdminActions() {
	var n int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_actions WHERE state!='pending'`).Scan(&n)
	if n <= maxAdminActionTombstones {
		return
	}
	_, _ = a.store.DB().Exec(`DELETE FROM admin_action_receipts WHERE action_id IN (SELECT id FROM admin_actions WHERE state!='pending' ORDER BY COALESCE(finished_at,made_at) ASC LIMIT ?)`, n-maxAdminActionTombstones)
	_, _ = a.store.DB().Exec(`DELETE FROM admin_actions WHERE id IN (SELECT id FROM admin_actions WHERE state!='pending' ORDER BY COALESCE(finished_at,made_at) ASC LIMIT ?)`, n-maxAdminActionTombstones)
}

// ---------------------------------------------------------------- web hooks

// adminActionForUser makes a card for every phone of a user (block, unblock,
// off, active, admin badge, wifi rule for the user).
func (a *API) adminActionForUser(action, userID string, admin AdminAccount, inner *signedBlob, detailFor func(deviceID string) map[string]any, words string) {
	if userID == "" {
		return
	}
	targets := []adminActionTarget{}
	for _, d := range a.activePhonesOf(userID) {
		t := adminActionTarget{Device: d, User: userID}
		if detailFor != nil {
			t.Detail = detailFor(d)
		}
		targets = append(targets, t)
	}
	_, _ = a.makeServerAction(newAdminAction{Action: action, By: admin.Username, AdminID: admin.ID,
		Label: "@" + a.usernameOf(userID), Targets: targets, Inner: inner, Detail: words})
}

// adminActionForDevice makes a card for one phone (sign out, remove phone,
// wifi rule for one phone).
func (a *API) adminActionForDevice(action, deviceID string, admin AdminAccount, detail map[string]any, words string) {
	if deviceID == "" {
		return
	}
	userID, name := a.deviceUser(deviceID)
	label := name
	if userID != "" {
		label = "@" + a.usernameOf(userID) + " \u00b7 " + name
	}
	_, _ = a.makeServerAction(newAdminAction{Action: action, By: admin.Username, AdminID: admin.ID, Label: label,
		Targets: []adminActionTarget{{Device: deviceID, User: userID, Detail: detail}}, Detail: words})
}

func (a *API) currentBlockBlob(userID string) *signedBlob {
	var b signedBlob
	if a.store.DB().QueryRow(`SELECT body,sig FROM admin_blocks WHERE target_user_id=?`, userID).Scan(&b.Body, &b.Sig) != nil {
		return nil
	}
	return &b
}

func (a *API) currentBadgeBlob(userID string) *signedBlob {
	var b signedBlob
	if a.store.DB().QueryRow(`SELECT body,sig FROM admin_badges WHERE user_id=?`, userID).Scan(&b.Body, &b.Sig) != nil {
		return nil
	}
	return &b
}

func (a *API) wifiDetail(deviceID string) map[string]any {
	p, err := a.effectiveNetworkPolicy(deviceID)
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(mustJSON(p), &m)
	return map[string]any{"policy": m}
}

// adminActionForStatus is the one-line hook in AdminSetUserStatus.
func (a *API) adminActionForStatus(userID, before, status string, admin AdminAccount) {
	action := ""
	switch {
	case status == "suspended" && before != "suspended":
		action = "block"
	case status == "active" && before == "suspended":
		action = "unblock"
	case status == "disabled" && before != "disabled":
		action = "off"
		if before != "suspended" {
			// Off also blocks on phones (read-only), so phones with no
			// internet learn it with the 212 block card too.
			_ = a.serverAdminBlock(userID, "block", admin.Username)
		}
	case status == "active" && before == "disabled":
		action = "active"
	}
	if action == "" {
		return
	}
	a.adminActionForUser(action, userID, admin, a.currentBlockBlob(userID), nil, "")
}

// adminActionForSession: "Sign out" on the web reaches the phone of that
// session even with no internet.
func (a *API) adminActionForSession(sessionID string, admin AdminAccount) {
	var deviceID string
	if a.store.DB().QueryRow(`SELECT device_id FROM auth_sessions WHERE id=?`, sessionID).Scan(&deviceID) != nil {
		return
	}
	a.adminActionForDevice("sign_out", deviceID, admin, map[string]any{"session_id": sessionID}, "")
}

// adminActionForVersionRule: a version rule is for every phone, so it has no
// boxes and no Done; the page shows "Reached N".
func (a *API) adminActionForVersionRule(rec versionRuleBody, blob signedBlob, admin AdminAccount) {
	b := blob
	words := rec.Mode + " below " + rec.MinVersion
	_, _ = a.makeServerAction(newAdminAction{Action: "version_rule", By: admin.Username, AdminID: admin.ID,
		Label: "every phone", Inner: &b, Detail: words, NoNeed: true})
}

// adminActionServerOnly lists a server-only action on the Admin actions page
// as "Done at once". No card goes to phones (nothing to do there).
func (a *API) adminActionServerOnly(action, deviceID string, admin AdminAccount, words string) {
	userID, name := a.deviceUser(deviceID)
	label := name
	if userID != "" {
		label = name + " (@" + a.usernameOf(userID) + ")"
	}
	_, _ = a.makeServerAction(newAdminAction{Action: action, By: admin.Username, AdminID: admin.ID, Label: label, Detail: words})
}

// adminActionForBlockRecord (Bundle 220): a block made on an OLD admin app
// (no own action card) still gets a card the web can follow.
func (a *API) adminActionForBlockRecord(rec adminBlockBody, blob signedBlob) {
	if rec.Signer != "admin" || rec.Cert == nil {
		return
	}
	var cert adminCertBody
	if json.Unmarshal([]byte(rec.Cert.Body), &cert) != nil {
		return
	}
	var build int64
	_ = a.store.DB().QueryRow(`SELECT build FROM device_builds WHERE device_id=?`, cert.DeviceID).Scan(&build)
	if build >= adminActionAppBuild {
		return // the new admin app sends its own card
	}
	targets := []adminActionTarget{}
	for _, d := range a.activePhonesOf(rec.TargetUserID) {
		targets = append(targets, adminActionTarget{Device: d, User: rec.TargetUserID})
	}
	b := blob
	_, _ = a.makeServerAction(newAdminAction{Action: rec.Action, By: cert.Username, AdminID: cert.AdminID,
		Label: "@" + a.usernameOf(rec.TargetUserID), Targets: targets, Inner: &b, Detail: "made on an old admin app"})
}

// ------------------------------------------------------------- incoming cards

// checkAdminAction returns the parsed card when it is genuine.
func (a *API) checkAdminAction(blob signedBlob) (adminActionBody, string) {
	var c adminActionBody
	if len(blob.Body) > maxAdminActionCardBytes {
		return c, "too_big"
	}
	if err := json.Unmarshal([]byte(blob.Body), &c); err != nil {
		return c, "bad_json"
	}
	if c.V != 1 || c.Kind != "admin_action" || strings.TrimSpace(c.ID) == "" || !adminActionNames[c.Action] {
		return c, "bad_fields"
	}
	if _, err := time.Parse(time.RFC3339Nano, c.At); err != nil {
		return c, "bad_time"
	}
	if c.Action == "cancel" && strings.TrimSpace(c.Cancels) == "" {
		return c, "bad_fields"
	}
	switch c.Signer {
	case "server":
		if !a.serverVerify(blob) {
			return c, "bad_server_sig"
		}
		return c, ""
	case "admin":
		if c.Cert == nil || !a.serverVerify(*c.Cert) {
			return c, "bad_cert"
		}
		var cert adminCertBody
		if err := json.Unmarshal([]byte(c.Cert.Body), &cert); err != nil || cert.Kind != "admin_cert" {
			return c, "bad_cert"
		}
		var ended int
		_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_cert_ended WHERE cert_id=?`, cert.CertID).Scan(&ended)
		if ended > 0 {
			return c, "cert_ended"
		}
		want := c.Action
		if c.Action == "cancel" {
			want = "owner"
			var cancelled string
			if a.store.DB().QueryRow(`SELECT action FROM admin_actions WHERE id=?`, c.Cancels).Scan(&cancelled) == nil {
				want = cancelled
			}
		}
		if want == "owner" {
			if cert.Role != "owner" {
				return c, "not_allowed"
			}
		} else if !adminRoleCan(cert.Role, want) {
			return c, "not_allowed"
		}
		if !verifyWithKey(cert.SigningKey, blob) {
			return c, "bad_sig"
		}
		return c, ""
	default:
		return c, "bad_signer"
	}
}

// storeIncomingAction saves a card made on an admin phone (or a server card a
// phone carried back). It returns true when the card was new; capped is true
// when the admin already has maxPendingPerAdmin pending actions.
func (a *API) storeIncomingAction(c adminActionBody, blob signedBlob) bool {
	ok, _ := a.storeIncomingActionCap(c, blob)
	return ok
}

func (a *API) storeIncomingActionCap(c adminActionBody, blob signedBlob) (bool, bool) {
	var exists int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_actions WHERE id=?`, c.ID).Scan(&exists)
	if exists > 0 {
		return false, false
	}
	if c.Signer == "server" {
		return false, false // server cards are always made here first
	}
	var cert adminCertBody
	_ = json.Unmarshal([]byte(c.Cert.Body), &cert)
	var live, mine int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_actions WHERE state='pending'`).Scan(&live)
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_actions WHERE state='pending' AND made_by=?`, cert.Username).Scan(&mine)
	if c.Action != "cancel" && (live >= maxLiveAdminActions || mine >= maxPendingPerAdmin) {
		return false, true
	}
	state := "pending"
	need := c.Need
	if c.Action == "cancel" {
		state, need = "done", 0
	}
	label := "(made offline \u00b7 details when the admin's phone syncs)"
	_, err := a.store.DB().Exec(`INSERT INTO admin_actions(id,action,body,sig,signer,made_by,made_at,need,done,state,parent,target_label,targets_json,detail,offline,finished_at)
		VALUES(?,?,?,?,'admin',?,?,?,0,?,?,?,'[]','',1,?)`,
		c.ID, c.Action, blob.Body, blob.Sig, cert.Username, c.At, need, state, c.Parent, label, nullIf(state != "done", nowStamp()))
	if err != nil {
		return false, false
	}
	a.adminAudit(cert.AdminID, "admin_action_offline", c.ID, "action="+c.Action+" by="+cert.Username+" made on the admin phone, reached the server")
	if c.Action == "cancel" {
		a.cancelAdminAction(c.Cancels, cert.Username, cert.AdminID, false)
	}
	// Receipts that came before the card are counted now.
	a.recountAdminAction(c.ID)
	return true, false
}

type adminActionLabel struct {
	ID      string         `json:"id"`
	UserID  string         `json:"user_id"`
	Label   string         `json:"label"`
	Devices []string       `json:"devices"`
	Detail  map[string]any `json:"detail,omitempty"`
	Words   string         `json:"words,omitempty"`
}

// applyAdminActionLabel: the admin's own phone tells the server (signed-in,
// over TLS) who an offline card is for. Relay phones never learn it. The
// person's other phones that the admin's phone didn't know get a companion
// card.
func (a *API) applyAdminActionLabel(fromDevice string, l adminActionLabel) {
	var body, signer, action string
	var offline int
	if a.store.DB().QueryRow(`SELECT body,signer,action,offline FROM admin_actions WHERE id=?`, l.ID).Scan(&body, &signer, &action, &offline) != nil || signer != "admin" || offline != 1 {
		return
	}
	var c adminActionBody
	if json.Unmarshal([]byte(body), &c) != nil || c.Cert == nil {
		return
	}
	var cert adminCertBody
	if json.Unmarshal([]byte(c.Cert.Body), &cert) != nil || cert.DeviceID != fromDevice {
		return
	}
	targets := []adminActionTarget{}
	known := map[string]bool{}
	for _, d := range l.Devices {
		known[d] = true
		targets = append(targets, adminActionTarget{Device: d, User: l.UserID, Boxed: true})
	}
	label := strings.TrimSpace(l.Label)
	if label == "" && l.UserID != "" {
		label = "@" + a.usernameOf(l.UserID)
	}
	if len(label) > 120 {
		label = label[:120]
	}
	words := l.Words
	if len(words) > 200 {
		words = words[:200]
	}
	tj, _ := json.Marshal(targets)
	res, _ := a.store.DB().Exec(`UPDATE admin_actions SET target_label=?,targets_json=?,detail=? WHERE id=? AND targets_json='[]'`, label, string(tj), words, l.ID)
	if res == nil || resAffected(res) == 0 {
		return // labels come once
	}
	perUser := map[string]bool{"block": true, "unblock": true, "off": true, "active": true, "admin_badge": true, "wifi_rule": true}
	if !perUser[action] || l.UserID == "" {
		return
	}
	if action == "wifi_rule" && len(l.Devices) != 0 && l.Detail == nil {
		return
	}
	extra := []adminActionTarget{}
	for _, d := range a.activePhonesOf(l.UserID) {
		if !known[d] && d != fromDevice {
			extra = append(extra, adminActionTarget{Device: d, User: l.UserID, Detail: l.Detail})
		}
	}
	if len(extra) == 0 {
		return
	}
	_, _ = a.makeServerAction(newAdminAction{Action: action, By: cert.Username, AdminID: cert.AdminID, Label: label,
		Targets: extra, Inner: c.Inner, Parent: l.ID, Detail: "other phones of the same person"})
}

// ------------------------------------------------------------------ receipts

// applyAdminActionReceipt saves a signed "done" from the target phone.
// uploader is the phone that brought it (the target itself or a carrier).
func (a *API) applyAdminActionReceipt(blob signedBlob, uploader string) bool {
	if len(blob.Body) > 4<<10 {
		return false
	}
	var d adminActionDoneBody
	if json.Unmarshal([]byte(blob.Body), &d) != nil || d.V != 1 || d.Kind != "admin_action_done" ||
		strings.TrimSpace(d.ActionID) == "" || strings.TrimSpace(d.Device) == "" || strings.TrimSpace(d.Result) == "" {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, d.At); err != nil {
		return false
	}
	verified := 0
	var key string
	_ = a.store.DB().QueryRow(`SELECT signing_key FROM device_signing_keys WHERE device_id=?`, d.Device).Scan(&key)
	if uploader == d.Device {
		verified = 1 // the target phone itself, by its device sign-in
	} else if key != "" {
		if !verifyWithKey(key, blob) {
			return false
		}
		verified = 1
	}
	via := "carried"
	if uploader == d.Device {
		via = "server"
	}
	switch d.Via {
	case "server", "wifi", "wifi_direct", "mesh":
		via = d.Via
	}
	res, err := a.store.DB().Exec(`INSERT INTO admin_action_receipts(action_id,device_id,result,at,body,sig,via,verified,received_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(action_id,device_id) DO NOTHING`,
		d.ActionID, d.Device, d.Result, d.At, blob.Body, blob.Sig, via, verified, nowStamp())
	if err != nil || resAffected(res) == 0 {
		return false
	}
	if verified == 1 {
		a.adminAudit("", "admin_action_done", d.ActionID, "device="+d.Device+" result="+d.Result+" via="+via)
	}
	a.recountAdminAction(d.ActionID)
	return true
}

// recheckReceipts verifies saved receipts once the phone's signing key is
// known (a carried receipt can arrive before the key).
func (a *API) recheckReceipts(deviceID, key string) {
	rows, err := a.store.DB().Query(`SELECT action_id,body,sig FROM admin_action_receipts WHERE device_id=? AND verified=0`, deviceID)
	if err != nil {
		return
	}
	type rc struct{ id, body, sig string }
	list := []rc{}
	for rows.Next() {
		var x rc
		if rows.Scan(&x.id, &x.body, &x.sig) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	for _, x := range list {
		if verifyWithKey(key, signedBlob{Body: x.body, Sig: x.sig}) {
			_, _ = a.store.DB().Exec(`UPDATE admin_action_receipts SET verified=1 WHERE action_id=? AND device_id=?`, x.id, deviceID)
		} else {
			_, _ = a.store.DB().Exec(`DELETE FROM admin_action_receipts WHERE action_id=? AND device_id=?`, x.id, deviceID)
		}
		a.recountAdminAction(x.id)
	}
}

func (a *API) rememberSigningKey(deviceID, key string) {
	key = strings.TrimSpace(key)
	if deviceID == "" || key == "" || len(key) > 128 {
		return
	}
	if raw, err := unb64url(key); err != nil || len(raw) != 32 {
		return
	}
	var cur string
	_ = a.store.DB().QueryRow(`SELECT signing_key FROM device_signing_keys WHERE device_id=?`, deviceID).Scan(&cur)
	if cur == key {
		return
	}
	if cur != "" {
		// The first key per phone is kept; a different one is only logged.
		a.auditSecurity("SIGNING_KEY_CHANGED", "", deviceID, "", "a different signing key was ignored")
		return
	}
	_, _ = a.store.DB().Exec(`INSERT INTO device_signing_keys(device_id,signing_key,first_seen_at) VALUES(?,?,?) ON CONFLICT(device_id) DO NOTHING`, deviceID, key, nowStamp())
	a.recheckReceipts(deviceID, key)
}

func (a *API) recountAdminAction(id string) {
	var state string
	var need int
	if a.store.DB().QueryRow(`SELECT state,need FROM admin_actions WHERE id=?`, id).Scan(&state, &need) != nil {
		return
	}
	var done int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_action_receipts WHERE action_id=? AND verified=1`, id).Scan(&done)
	_, _ = a.store.DB().Exec(`UPDATE admin_actions SET done=? WHERE id=?`, done, id)
	if state == "pending" && need > 0 && done >= need {
		_, _ = a.store.DB().Exec(`UPDATE admin_actions SET state='done',finished_at=? WHERE id=?`, nowStamp(), id)
		a.adminAudit("", "admin_action_finished", id, "done="+strconv.Itoa(done)+" need="+strconv.Itoa(need))
		a.signActionState(id)
		a.hintAdminActions()
	}
}

// cancelAdminAction stops a pending action. withCard makes a server cancel
// card too (web cancel); a cancel made on an admin phone is its own card.
func (a *API) cancelAdminAction(id, by, adminID string, withCard bool) bool {
	res, err := a.store.DB().Exec(`UPDATE admin_actions SET state='cancelled',cancelled_at=?,finished_at=? WHERE (id=? OR parent=?) AND state='pending'`, nowStamp(), nowStamp(), id, id)
	if err != nil || resAffected(res) == 0 {
		return false
	}
	a.signActionState(id)
	if rows, err := a.store.DB().Query(`SELECT id FROM admin_actions WHERE parent=?`, id); err == nil {
		kids := []string{}
		for rows.Next() {
			var k string
			if rows.Scan(&k) == nil {
				kids = append(kids, k)
			}
		}
		rows.Close()
		for _, k := range kids {
			a.signActionState(k)
		}
	}
	if withCard {
		_, _ = a.makeServerAction(newAdminAction{Action: "cancel", By: by, AdminID: adminID, Label: id, Cancels: id, NoNeed: true})
	}
	a.adminAudit(adminID, "admin_action_cancel", id, "by="+by)
	a.hintAdminActions()
	return true
}

// ----------------------------------------------------------------- handlers

func (a *API) adminActionStates(ids []string) []signedBlob {
	out := []signedBlob{}
	for _, id := range ids {
		var b signedBlob
		if a.store.DB().QueryRow(`SELECT state_body,state_sig FROM admin_actions WHERE id=? AND state!='pending' AND state_body!=''`, id).Scan(&b.Body, &b.Sig) == nil {
			out = append(out, b)
		}
	}
	return out
}

type adminActionsSyncIn struct {
	Cards      []signedBlob       `json:"cards"`
	Receipts   []signedBlob       `json:"receipts"`
	Have       []string           `json:"have"`
	SigningKey string             `json:"signing_key"`
	Labels     []adminActionLabel `json:"labels"`
	Build      int64              `json:"build"`
}

func (a *API) takeAdminActionsIn(deviceID string, in *adminActionsSyncIn) (int, int, []string) {
	if len(in.Cards) > 200 {
		in.Cards = in.Cards[:200]
	}
	if len(in.Receipts) > 500 {
		in.Receipts = in.Receipts[:500]
	}
	if len(in.Have) > 2000 {
		in.Have = in.Have[:2000]
	}
	if len(in.Labels) > 100 {
		in.Labels = in.Labels[:100]
	}
	a.rememberSigningKey(deviceID, in.SigningKey)
	applied, rejected := 0, 0
	capped := []string{}
	for _, blob := range in.Cards {
		c, reason := a.checkAdminAction(blob)
		if reason != "" {
			rejected++
			continue
		}
		ok, over := a.storeIncomingActionCap(c, blob)
		if ok {
			applied++
		}
		if over {
			capped = append(capped, c.ID)
		}
	}
	for _, l := range in.Labels {
		a.applyAdminActionLabel(deviceID, l)
	}
	for _, rc := range in.Receipts {
		a.applyAdminActionReceipt(rc, deviceID)
	}
	return applied, rejected, capped
}

// AdminActionsSync: any signed-in phone sends the cards, receipts and labels
// it carries and gets the pending cards it doesn't have plus the "stop" notes
// for the cards it has.
func (a *API) AdminActionsSync(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var in adminActionsSyncIn
	if err := decodeJSON(r, &in, 4<<20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	applied, rejected, capped := a.takeAdminActionsIn(deviceID, &in)
	have := map[string]bool{}
	for _, id := range in.Have {
		have[id] = true
	}
	cards := []signedBlob{}
	more := false
	rows, err := a.store.DB().Query(`SELECT id,body,sig FROM admin_actions WHERE state='pending' ORDER BY made_at ASC`)
	if err == nil {
		for rows.Next() {
			var id string
			var b signedBlob
			if rows.Scan(&id, &b.Body, &b.Sig) != nil || have[id] {
				continue
			}
			if len(cards) >= adminActionSyncPage {
				more = true
				break
			}
			cards = append(cards, b)
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cards":    cards,
		"states":   a.adminActionStates(in.Have),
		"more":     more,
		"applied":  applied,
		"rejected": rejected,
		// Bundle 220: the admin already has 50 pending actions; the app shows
		// "Finish or cancel some actions first".
		"too_many_pending": capped,
	})
}

// AdminActionsMine: a phone that can't pass device auth (blocked, closed or
// signed out by an admin) still gets the cards made for it and can send its
// receipts. A removed phone gets nothing here; it learns from other phones.
func (a *API) AdminActionsMine(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.Header.Get("X-Device-ID"))
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if id == "" || token == "" {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var storedHash, deviceStatus string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(token_hash,''),COALESCE(status,'active') FROM devices WHERE id=?`, id).Scan(&storedHash, &deviceStatus); err != nil ||
		storedHash == "" || !secureTokenEqual(storedHash, tokenHash(token)) || deviceStatus != "active" {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var in adminActionsSyncIn
	if err := decodeJSON(r, &in, 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.Cards, in.Labels = nil, nil // only its own receipts
	_, _, _ = a.takeAdminActionsIn(id, &in)
	cards := []signedBlob{}
	ids := []string{}
	rows, err := a.store.DB().Query(`SELECT id,body,sig,targets_json,state FROM admin_actions WHERE targets_json LIKE ? ORDER BY made_at ASC LIMIT 200`, "%\""+id+"\"%")
	if err == nil {
		for rows.Next() {
			var aid, tj, state string
			var b signedBlob
			if rows.Scan(&aid, &b.Body, &b.Sig, &tj, &state) != nil {
				continue
			}
			ids = append(ids, aid)
			if state == "pending" {
				cards = append(cards, b)
			}
		}
		rows.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards, "states": a.adminActionStates(ids)})
}

type adminActionPhone struct {
	Device string `json:"device"`
	Name   string `json:"name"`
	Result string `json:"result"`
	At     string `json:"at"`
	Via    string `json:"via"`
	OldApp bool   `json:"old_app"`
	NoKey  bool   `json:"no_key"`
}

type adminActionRow struct {
	ID         string             `json:"id"`
	Action     string             `json:"action"`
	Label      string             `json:"label"`
	State      string             `json:"state"`
	Done       int                `json:"done"`
	Need       int                `json:"need"`
	Reached    int                `json:"reached"`
	MadeBy     string             `json:"made_by"`
	MadeAt     string             `json:"made_at"`
	Offline    bool               `json:"offline"`
	FinishedAt string             `json:"finished_at,omitempty"`
	Detail     string             `json:"detail,omitempty"`
	OldApp     int                `json:"old_app"`
	Via        string             `json:"via,omitempty"`
	ServerOnly bool               `json:"server_only"`
	Phones     []adminActionPhone `json:"phones"`
}

func (a *API) adminActionRowFor(id string) (adminActionRow, bool) {
	var row adminActionRow
	var tj string
	var offline int
	var fin sql.NullString
	if a.store.DB().QueryRow(`SELECT id,action,target_label,state,done,need,made_by,made_at,offline,finished_at,detail,targets_json FROM admin_actions WHERE id=?`, id).
		Scan(&row.ID, &row.Action, &row.Label, &row.State, &row.Done, &row.Need, &row.MadeBy, &row.MadeAt, &offline, &fin, &row.Detail, &tj) != nil {
		return row, false
	}
	row.Offline = offline == 1
	row.FinishedAt = fin.String
	row.ServerOnly = serverOnlyActions[row.Action]
	targets := []adminActionTarget{}
	_ = json.Unmarshal([]byte(tj), &targets)
	// Receipts (all, verified ones count).
	got := map[string]adminActionPhone{}
	if rows, err := a.store.DB().Query(`SELECT device_id,result,at,via FROM admin_action_receipts WHERE action_id=? AND verified=1`, id); err == nil {
		for rows.Next() {
			var p adminActionPhone
			if rows.Scan(&p.Device, &p.Result, &p.At, &p.Via) == nil {
				got[p.Device] = p
			}
		}
		rows.Close()
	}
	row.Reached = len(got)
	seen := map[string]bool{}
	for _, t := range targets {
		p, ok := got[t.Device]
		if !ok {
			p = adminActionPhone{Device: t.Device}
		}
		_, p.Name = a.deviceUser(t.Device)
		p.NoKey = !t.Boxed
		if !ok {
			var build int64
			_ = a.store.DB().QueryRow(`SELECT build FROM device_builds WHERE device_id=?`, t.Device).Scan(&build)
			if build > 0 && build < adminActionAppBuild {
				p.OldApp = true
				row.OldApp++
			}
		}
		seen[t.Device] = true
		row.Phones = append(row.Phones, p)
	}
	if row.Action != "version_rule" {
		for d, p := range got {
			if !seen[d] {
				_, p.Name = a.deviceUser(d)
				row.Phones = append(row.Phones, p)
			}
		}
	}
	if row.Phones == nil {
		row.Phones = []adminActionPhone{}
	}
	for _, p := range row.Phones {
		if p.Via != "" {
			row.Via = p.Via
		}
	}
	return row, true
}

// AdminActionsList: GET /api/v1/admin/actions?state=pending|done|cancelled|all
func (a *API) AdminActionsList(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	q := `SELECT id FROM admin_actions WHERE parent='' AND action!='cancel'`
	args := []any{}
	switch state {
	case "pending", "done", "cancelled":
		if state == "done" {
			q += ` AND state IN ('done','replaced')`
		} else {
			q += ` AND state=?`
			args = append(args, state)
		}
	}
	q += ` ORDER BY made_at DESC LIMIT 300`
	rows, err := a.store.DB().Query(q, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list admin actions")
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []adminActionRow{}
	for _, id := range ids {
		row, ok := a.adminActionRowFor(id)
		if !ok {
			continue
		}
		// Companion cards add their phones to the parent row.
		if kids, err := a.store.DB().Query(`SELECT id FROM admin_actions WHERE parent=?`, id); err == nil {
			kidIDs := []string{}
			for kids.Next() {
				var k string
				if kids.Scan(&k) == nil {
					kidIDs = append(kidIDs, k)
				}
			}
			kids.Close()
			for _, k := range kidIDs {
				if kr, ok := a.adminActionRowFor(k); ok {
					row.Need += kr.Need
					row.Done += kr.Done
					row.Reached += kr.Reached
					row.OldApp += kr.OldApp
					row.Phones = append(row.Phones, kr.Phones...)
					if kr.State == "pending" && row.State == "done" {
						row.State = "pending"
					}
				}
			}
		}
		out = append(out, row)
	}
	var pending int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_actions WHERE parent='' AND action!='cancel' AND state='pending'`).Scan(&pending)
	writeJSON(w, http.StatusOK, map[string]any{"actions": out, "pending": pending})
}

// AdminActionCancel: POST /api/v1/admin/actions/{id}/cancel
func (a *API) AdminActionCancel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	var action, state string
	if id == "" || a.store.DB().QueryRow(`SELECT action,state FROM admin_actions WHERE id=?`, id).Scan(&action, &state) != nil {
		if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
			return
		}
		writeError(w, http.StatusNotFound, "action not found")
		return
	}
	perm := adminActionPermission(action)
	if perm == "owner" {
		perm = "manage_settings" // owner only
	}
	admin, ok := a.requireAdminPermission(w, r, perm)
	if !ok {
		return
	}
	if adminActionPermission(action) == "owner" && admin.Role != "owner" {
		writeError(w, http.StatusForbidden, "owner only")
		return
	}
	if state != "pending" {
		writeErrorCode(w, http.StatusConflict, "not_pending", "this action is not pending")
		return
	}
	if action == "version_rule" {
		writeErrorCode(w, http.StatusConflict, "use_app_version_page", "change the rule on the App version page")
		return
	}
	if !a.cancelAdminAction(id, admin.Username, admin.ID, true) {
		writeErrorCode(w, http.StatusConflict, "not_pending", "this action is not pending")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "cancelled"})
}
