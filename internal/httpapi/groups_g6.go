package httpapi

// Bundle G6: group invite link (server groups only).
//
//   - POST /api/v1/groups/invite?group_id=  {approve, reset} (admins)
//     -> {token, link, approve}. One link per group; "reset" makes a new
//     token so the old link stops working. "approve" = people who open the
//     link ask first (the app sends true for groups that also work Nearby:
//     only an admin phone can add them to the signed group card).
//   - GET  /api/v1/groups/join?token=  -> preview for anyone signed in.
//   - POST /api/v1/groups/join?token=  {seal, sign} (optional, the asker's
//     public keys for the signed Nearby card) -> {status: member|joined|requested}.
//   - GET  /api/v1/groups/join-requests?group_id=  (admins, last 24 h).
//   - DELETE /api/v1/groups/join-requests?group_id=&device_id= (admins,
//     silent decline). Approve = the normal "add member" call.
//   - GET  /join/{token}  small public page with an "Open in LinkUp" button
//     (chat apps make https links tappable, not linkup://).

import (
	"crypto/rand"
	"database/sql"
	"html"
	"math/big"
	"net/http"
	"strings"
	"time"
)

const groupJoinRequestTTL = 24 * time.Hour

const inviteTokenChars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

func newInviteToken() (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(inviteTokenChars)))
	for i := 0; i < 16; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(inviteTokenChars[n.Int64()])
	}
	return b.String(), nil
}

func validInviteToken(t string) bool {
	if len(t) < 8 || len(t) > 64 {
		return false
	}
	for _, c := range t {
		if !strings.ContainsRune(inviteTokenChars, c) {
			return false
		}
	}
	return true
}

type groupInvite struct {
	GroupID   string
	Token     string
	Approve   bool
	CreatedBy string
}

func (a *API) inviteByToken(token string) (groupInvite, error) {
	var in groupInvite
	var approve int
	err := a.store.DB().QueryRow(`SELECT group_id,token,approve,created_by FROM group_invites WHERE token=?`, token).
		Scan(&in.GroupID, &in.Token, &approve, &in.CreatedBy)
	in.Approve = approve == 1
	return in, err
}

func (a *API) hasJoinRequest(gid, device string) bool {
	since := time.Now().UTC().Add(-groupJoinRequestTTL).Format(time.RFC3339Nano)
	var n int
	return a.store.DB().QueryRow(`SELECT 1 FROM group_join_requests WHERE group_id=? AND device_id=? AND created_at>?`, gid, device, since).Scan(&n) == nil
}

func inviteLink(token string) string { return "linkup://join/" + token }

// GroupInvite: POST /api/v1/groups/invite?group_id= (admins).
func (a *API) GroupInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	gid := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if gid == "" {
		writeError(w, 400, "group_id is required")
		return
	}
	role := a.groupRole(gid, actor)
	if role == "" {
		writeGroupError(w, 403, groupErrNotMember, "device is not a group member")
		return
	}
	if !isAdminRole(role) {
		writeGroupError(w, 403, groupErrAdminsOnly, "only admins can share the invite link")
		return
	}
	var in struct {
		Approve bool `json:"approve"`
		Reset   bool `json:"reset"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &in, 1024); err != nil {
			writeError(w, 400, "invalid request body")
			return
		}
	}
	approve := 0
	if in.Approve {
		approve = 1
	}
	var token string
	err := a.store.DB().QueryRow(`SELECT token FROM group_invites WHERE group_id=?`, gid).Scan(&token)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	switch {
	case err == sql.ErrNoRows || (err == nil && in.Reset):
		if token, err = newInviteToken(); err != nil {
			writeError(w, 500, "failed to make the link")
			return
		}
		_, err = a.store.DB().Exec(`INSERT INTO group_invites(group_id,token,approve,created_by,created_at) VALUES(?,?,?,?,?)
			ON CONFLICT(group_id) DO UPDATE SET token=excluded.token, approve=excluded.approve, created_by=excluded.created_by, created_at=excluded.created_at`,
			gid, token, approve, actor, now)
	case err == nil:
		_, err = a.store.DB().Exec(`UPDATE group_invites SET approve=? WHERE group_id=?`, approve, gid)
	}
	if err != nil {
		writeError(w, 500, "failed to save the link")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "token": token, "link": inviteLink(token), "approve": in.Approve})
}

type groupJoinPreview struct {
	GroupID     string   `json:"group_id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Members     int      `json:"members"`
	MemberNames []string `json:"member_names"`
	InvitedBy   string   `json:"invited_by,omitempty"`
	Approve     bool     `json:"approve"`
	Status      string   `json:"status"` // member | requested | none
}

func (a *API) joinPreview(in groupInvite, device string) (groupJoinPreview, error) {
	g, err := a.loadGroup(in.GroupID)
	if err != nil {
		return groupJoinPreview{}, err
	}
	p := groupJoinPreview{GroupID: g.ID, Name: g.Name, Description: g.Description, Approve: in.Approve, MemberNames: []string{}}
	rows, err := a.store.DB().Query(`SELECT `+memberNameSQL+` FROM group_members gm
		JOIN devices d ON d.id=gm.device_id LEFT JOIN profiles p ON p.user_id=d.user_id
		WHERE gm.group_id=? ORDER BY CASE gm.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, gm.joined_at`, g.ID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			p.Members++
			if len(p.MemberNames) < 4 {
				p.MemberNames = append(p.MemberNames, strings.TrimSpace(name))
			}
		}
	}
	tx, err := a.store.DB().Begin()
	if err == nil {
		p.InvitedBy = a.deviceNamesTx(tx, []string{in.CreatedBy})[in.CreatedBy]
		_ = tx.Rollback()
	}
	switch {
	case a.isGroupMember(g.ID, device):
		p.Status = "member"
	case a.hasJoinRequest(g.ID, device):
		p.Status = "requested"
	default:
		p.Status = "none"
	}
	return p, nil
}

func (a *API) inviteFromQuery(w http.ResponseWriter, r *http.Request) (string, groupInvite, bool) {
	device, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return "", groupInvite{}, false
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if !validInviteToken(token) {
		writeGroupError(w, 404, "GROUP_LINK_INVALID", "this invite link doesn't work any more")
		return "", groupInvite{}, false
	}
	in, err := a.inviteByToken(token)
	if err != nil {
		writeGroupError(w, 404, "GROUP_LINK_INVALID", "this invite link doesn't work any more")
		return "", groupInvite{}, false
	}
	return device, in, true
}

// GroupJoinPreview: GET /api/v1/groups/join?token=
func (a *API) GroupJoinPreview(w http.ResponseWriter, r *http.Request) {
	device, in, ok := a.inviteFromQuery(w, r)
	if !ok {
		return
	}
	p, err := a.joinPreview(in, device)
	if err != nil {
		writeGroupError(w, 404, "GROUP_LINK_INVALID", "this invite link doesn't work any more")
		return
	}
	writeJSON(w, 200, p)
}

// GroupJoin: POST /api/v1/groups/join?token=
func (a *API) GroupJoin(w http.ResponseWriter, r *http.Request) {
	device, in, ok := a.inviteFromQuery(w, r)
	if !ok {
		return
	}
	if a.isGroupMember(in.GroupID, device) {
		writeJSON(w, 200, map[string]any{"ok": true, "status": "member", "group_id": in.GroupID})
		return
	}
	var count int
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM group_members WHERE group_id=?`, in.GroupID).Scan(&count); err != nil {
		writeError(w, 500, "failed to count group members")
		return
	}
	if count >= maxGroupMembers {
		writeGroupError(w, 400, "GROUP_FULL", "group cannot exceed 100 members")
		return
	}
	var keys struct {
		Seal string `json:"seal"`
		Sign string `json:"sign"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &keys, 2048); err != nil || !cardKeyOK(keys.Seal) || !cardKeyOK(keys.Sign) {
			writeError(w, 400, "invalid request body")
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if in.Approve {
		if _, err := a.store.DB().Exec(`INSERT INTO group_join_requests(group_id,device_id,seal,sign,created_at) VALUES(?,?,?,?,?)
			ON CONFLICT(group_id,device_id) DO UPDATE SET seal=excluded.seal, sign=excluded.sign, created_at=excluded.created_at`, in.GroupID, device, keys.Seal, keys.Sign, now); err != nil {
			writeError(w, 500, "failed to save the request")
			return
		}
		a.tellAdminsJoinRequest(in.GroupID, device, now)
		writeJSON(w, 200, map[string]any{"ok": true, "status": "requested", "group_id": in.GroupID})
		return
	}
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to join")
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT OR IGNORE INTO group_members(group_id,device_id,role,joined_at,added_by) VALUES(?,?,?,?,?)`, in.GroupID, device, "member", now, in.CreatedBy)
	if err != nil {
		writeError(w, 500, "failed to join")
		return
	}
	_, _ = tx.Exec(`DELETE FROM group_join_requests WHERE group_id=? AND device_id=?`, in.GroupID, device)
	if err = queueRosterTx(a, tx, in.GroupID); err != nil {
		writeError(w, 500, "failed to create group sync event")
		return
	}
	var ev GroupMessage
	var evTo []string
	if n, _ := res.RowsAffected(); n == 1 {
		if ev, evTo, err = a.emitGroupEventTx(tx, in.GroupID, groupEvent{Kind: "link_join", By: device}); err != nil {
			writeError(w, 500, "failed to store group event")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to join")
		return
	}
	a.sendGroupEvent(ev, evTo)
	writeJSON(w, 200, map[string]any{"ok": true, "status": "joined", "group_id": in.GroupID})
}

func (a *API) tellAdminsJoinRequest(gid, device, at string) {
	rows, err := a.store.DB().Query(`SELECT device_id FROM group_members WHERE group_id=? AND role IN ('owner','admin')`, gid)
	if err != nil {
		return
	}
	var admins []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			admins = append(admins, id)
		}
	}
	rows.Close()
	name := ""
	if tx, err := a.store.DB().Begin(); err == nil {
		name = a.deviceNamesTx(tx, []string{device})[device]
		_ = tx.Rollback()
	}
	payload := mustJSON(WSMessage{Type: "group_join_request", GroupID: gid, DeviceID: device, Body: name, CreatedAt: at})
	for _, id := range admins {
		a.hub.SendTo(id, payload)
	}
}

type groupJoinRequest struct {
	DeviceID  string `json:"device_id"`
	Name      string `json:"name"`
	Seal      string `json:"seal,omitempty"`
	Sign      string `json:"sign,omitempty"`
	CreatedAt string `json:"created_at"`
}

// cardKeyOK: empty or a short base64 / base64url public key.
func cardKeyOK(k string) bool {
	if len(k) > 200 {
		return false
	}
	for _, c := range k {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("+/=-_", c)) {
			return false
		}
	}
	return true
}

func (a *API) adminOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	actor, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return "", false
	}
	gid := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if gid == "" {
		writeError(w, 400, "group_id is required")
		return "", false
	}
	role := a.groupRole(gid, actor)
	if role == "" {
		writeGroupError(w, 403, groupErrNotMember, "device is not a group member")
		return "", false
	}
	if !isAdminRole(role) {
		writeGroupError(w, 403, groupErrAdminsOnly, "only admins can see join requests")
		return "", false
	}
	return gid, true
}

// GroupJoinRequests: GET /api/v1/groups/join-requests?group_id= (admins).
func (a *API) GroupJoinRequests(w http.ResponseWriter, r *http.Request) {
	gid, ok := a.adminOf(w, r)
	if !ok {
		return
	}
	since := time.Now().UTC().Add(-groupJoinRequestTTL).Format(time.RFC3339Nano)
	_, _ = a.store.DB().Exec(`DELETE FROM group_join_requests WHERE created_at<=?`, since)
	rows, err := a.store.DB().Query(`SELECT jr.device_id, COALESCE(`+memberNameSQL+`,''), jr.seal, jr.sign, jr.created_at FROM group_join_requests jr
		LEFT JOIN devices d ON d.id=jr.device_id LEFT JOIN profiles p ON p.user_id=d.user_id
		WHERE jr.group_id=? ORDER BY jr.created_at ASC LIMIT 100`, gid)
	if err != nil {
		writeError(w, 500, "failed to list requests")
		return
	}
	defer rows.Close()
	out := make([]groupJoinRequest, 0)
	for rows.Next() {
		var q groupJoinRequest
		if rows.Scan(&q.DeviceID, &q.Name, &q.Seal, &q.Sign, &q.CreatedAt) == nil {
			q.Name = strings.TrimSpace(q.Name)
			out = append(out, q)
		}
	}
	writeJSON(w, 200, map[string]any{"requests": out})
}

// GroupJoinDecline: DELETE /api/v1/groups/join-requests?group_id=&device_id=
func (a *API) GroupJoinDecline(w http.ResponseWriter, r *http.Request) {
	gid, ok := a.adminOf(w, r)
	if !ok {
		return
	}
	device := strings.TrimSpace(r.URL.Query().Get("device_id"))
	if device == "" {
		writeError(w, 400, "device_id is required")
		return
	}
	_, _ = a.store.DB().Exec(`DELETE FROM group_join_requests WHERE group_id=? AND device_id=?`, gid, device)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// GroupJoinPage: GET /join/{token} - public, tells nothing about the group.
func (a *API) GroupJoinPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	if !validInviteToken(token) {
		http.NotFound(w, r)
		return
	}
	t := html.EscapeString(token)
	intent := "intent://join/" + t + "#Intent;scheme=linkup;package=com.sajjad.linkup;end"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>LinkUp group invite</title>
<style>body{font-family:sans-serif;background:#F4F6FA;margin:0;display:flex;min-height:100vh;align-items:center;justify-content:center}
.c{background:#fff;border-radius:18px;padding:28px 24px;max-width:340px;text-align:center;box-shadow:0 4px 18px rgba(0,0,0,.08)}
a.b{display:block;background:#1E5BFF;color:#fff;text-decoration:none;padding:13px;border-radius:24px;font-weight:600;margin-top:18px}
p{color:#555;font-size:15px;line-height:1.4}code{font-size:13px;color:#1E5BFF}</style></head>
<body><div class="c"><h2>Group invite</h2><p>You were invited to a LinkUp group.</p>
<a class="b" href="` + intent + `">Open in LinkUp</a>
<p style="font-size:13px;margin-top:16px">No button? Open LinkUp and paste:<br><code>linkup://join/` + t + `</code></p></div></body></html>`))
}
