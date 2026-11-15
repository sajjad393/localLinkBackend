package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Bundle 314 (TODO_314):
//
//   Issue 1   a chat the Owner is in can't be asked (409 own_chat): it is
//             already on the Owner's phone. "Me" = AdminAccount.UserID; a
//             password-only web owner has no user, so nothing is "mine".
//             The group lists carry a `me` flag so the app and the web hide
//             those chats.
//   Issue 1b  one open request per chat: a pending get_chat for the same 2
//             people / the same group that is not Ready blocks a new one,
//             whatever the range (409 already_asked + open_id).
//   Issue 1c  member phones report their phone-only groups ("group notes":
//             id, name, member phones, made_at - no messages, no keys) on
//             their own endpoint (an old server answers 404 and the phone
//             keeps the note; the admin-actions sync refuses unknown
//             fields, so it can't carry them). The Owner sees them as
//             "Nearby" groups and can ask them like any group: the card goes
//             to the member phones of the note. A phone only answers for a
//             group it is really in, so a fake note can't leak messages.

const (
	groupNoteType314    = "group_note_314"
	groupNoteMaxBody314 = 16 << 10
	groupNoteMaxPerPost = 50
	groupNoteMaxMembers = 256
	groupNoteMaxName314 = 100
	groupNoteKeepFor314 = 60 * 24 * time.Hour
	ownChatText314      = "You are in this chat. It's already on your phone."
	alreadyAskedText314 = "This chat already has an open request. Open it or cancel it first."
	ownChatCode314      = "own_chat"
	alreadyAskedCode314 = "already_asked"
	groupNotesMaxListed = 500
	groupNoteFutureSkew = 24 * time.Hour
)

type groupNote314 struct {
	T       string   `json:"t"`
	V       int      `json:"v"`
	GID     string   `json:"gid"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
	MadeAt  string   `json:"made_at"`
	From    string   `json:"from"`
	At      string   `json:"at"`
}

// AdminNearbyGroup is one reported phone-only group in the Owner's list.
type AdminNearbyGroup struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Members    int      `json:"members"`
	MadeAt     string   `json:"made_at,omitempty"`
	ReportedAt string   `json:"reported_at"`
	Me         bool     `json:"me"`
	Devices    []string `json:"member_devices,omitempty"`
}

func (a *API) ensureGroupNotes314() {
	_, _ = a.store.DB().Exec(`CREATE TABLE IF NOT EXISTS group_notes_314(
		gid TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		members_json TEXT NOT NULL,
		made_at TEXT NOT NULL DEFAULT '',
		from_device TEXT NOT NULL,
		note_at TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL,
		sig TEXT NOT NULL,
		received_at TEXT NOT NULL)`)
}

// takeGroupNote314 checks and keeps one signed note. uploader = the phone
// sending it (it may carry another member's note). Returns "" when kept or
// already known, else why it was refused.
func (a *API) takeGroupNote314(blob signedBlob, uploader string, now time.Time) string {
	if len(blob.Body) == 0 || len(blob.Body) > groupNoteMaxBody314 {
		return "too_big"
	}
	var n groupNote314
	if json.Unmarshal([]byte(blob.Body), &n) != nil || n.T != groupNoteType314 || n.V != 1 {
		return "bad_json"
	}
	n.GID, n.From = strings.TrimSpace(n.GID), strings.TrimSpace(n.From)
	n.Name = strings.TrimSpace(n.Name)
	if n.GID == "" || len(n.GID) > 128 || n.From == "" || n.Name == "" || len(n.Members) == 0 || len(n.Members) > groupNoteMaxMembers {
		return "bad_fields"
	}
	if len([]rune(n.Name)) > groupNoteMaxName314 {
		n.Name = string([]rune(n.Name)[:groupNoteMaxName314])
	}
	members := []string{}
	seen := map[string]bool{}
	for _, m := range n.Members {
		m = strings.TrimSpace(m)
		if m == "" || len(m) > 128 || seen[m] {
			continue
		}
		seen[m] = true
		members = append(members, m)
	}
	sort.Strings(members)
	if !seen[n.From] {
		return "not_member" // only a member reports its group
	}
	if at, err := time.Parse(time.RFC3339Nano, n.At); err != nil || at.After(now.Add(groupNoteFutureSkew)) {
		return "bad_time"
	}
	var key string
	_ = a.store.DB().QueryRow(`SELECT signing_key FROM device_signing_keys WHERE device_id=?`, n.From).Scan(&key)
	switch {
	case key != "":
		if !verifyWithKey(key, blob) {
			return "bad_sig"
		}
	case uploader != n.From:
		return "unknown_key" // a carrier brought it: wait for the phone's own key
	}
	a.ensureGroupNotes314()
	var oldAt string
	if a.store.DB().QueryRow(`SELECT note_at FROM group_notes_314 WHERE gid=?`, n.GID).Scan(&oldAt) == nil && oldAt >= n.At {
		return "" // the newest note wins (same or older: nothing to do)
	}
	mj, _ := json.Marshal(members)
	if _, err := a.store.DB().Exec(`INSERT OR REPLACE INTO group_notes_314(gid,name,members_json,made_at,from_device,note_at,body,sig,received_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, n.GID, n.Name, string(mj), n.MadeAt, n.From, n.At, blob.Body, blob.Sig, nowStamp()); err != nil {
		return "store"
	}
	_, _ = a.store.DB().Exec(`DELETE FROM group_notes_314 WHERE received_at < ?`, now.Add(-groupNoteKeepFor314).UTC().Format(time.RFC3339Nano))
	return ""
}

// AdminGroupNotes: POST /api/v1/admin/group-notes (device sign-in)
// {"notes":[{body,sig}]} -> {"ok":true,"kept":n,"refused":{why:n}}.
func (a *API) AdminGroupNotes(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var in struct {
		Notes []signedBlob `json:"notes"`
	}
	if err := decodeJSON(r, &in, 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(in.Notes) > groupNoteMaxPerPost {
		in.Notes = in.Notes[:groupNoteMaxPerPost]
	}
	kept := 0
	refused := map[string]int{}
	now := time.Now()
	for _, b := range in.Notes {
		if why := a.takeGroupNote314(b, deviceID, now); why != "" {
			refused[why]++
		} else {
			kept++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kept": kept, "refused": refused})
}

// reportedGroup314 is a phone-only group known from a member's note.
func (a *API) reportedGroup314(gid string) (string, []string, bool) {
	a.ensureGroupNotes314()
	var name, mj string
	if a.store.DB().QueryRow(`SELECT name, members_json FROM group_notes_314 WHERE gid=?`, gid).Scan(&name, &mj) != nil {
		return "", nil, false
	}
	devices := []string{}
	_ = json.Unmarshal([]byte(mj), &devices)
	return name, devices, len(devices) > 0
}

// adminDevices314: the phones of the admin's own app user (none for a
// password-only web owner).
func (a *API) adminDevices314(admin AdminAccount) map[string]bool {
	out := map[string]bool{}
	if strings.TrimSpace(admin.UserID) == "" {
		return out
	}
	for _, d := range a.allPhonesOf(admin.UserID) {
		out[d] = true
	}
	return out
}

// ownChat314: the admin's own user is one of the two people / a member.
func (a *API) ownChat314(admin AdminAccount, pick chatPick) bool {
	me := strings.TrimSpace(admin.UserID)
	if me == "" {
		return false
	}
	if pick.ref.Kind == "direct" {
		for _, u := range pick.ref.Users {
			if u == me {
				return true
			}
		}
		return false
	}
	mine := a.adminDevices314(admin)
	for _, t := range pick.targets {
		if t.User == me || mine[t.Device] {
			return true
		}
	}
	return false
}

// sameChat314: the same 2 people (any order; user or device ids) or the
// same group.
func (a *API) sameChat314(x chatRef, y chatRef) bool {
	if x.Kind != y.Kind {
		return false
	}
	if x.Kind == "group" {
		return x.GroupID != "" && x.GroupID == y.GroupID
	}
	people := func(ref chatRef) map[string]bool {
		ids := ref.Users
		if len(ids) < 2 {
			ids = ref.Devices
		}
		out := map[string]bool{}
		for _, id := range ids {
			if u := a.resolveUserID(id); u != "" {
				out[u] = true
			} else if id != "" {
				out[id] = true
			}
		}
		return out
	}
	px, py := people(x), people(y)
	if len(px) != 2 || len(py) != 2 {
		return false
	}
	for id := range px {
		if !py[id] {
			return false
		}
	}
	return true
}

// openRequest314: the open (Sent / Reached / Coming) get_chat of this chat,
// or "". Ready, cancelled, done and "No messages" requests don't block.
func (a *API) openRequest314(ref chatRef) string {
	rows, err := a.store.DB().Query(`SELECT id FROM admin_actions WHERE action='get_chat' AND state='pending' ORDER BY made_at DESC LIMIT 500`)
	if err != nil {
		return ""
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
		old, _, _ := a.chatOf(id)
		if old == nil || !a.sameChat314(*old, ref) {
			continue
		}
		if answerID, _, of := a.bestAnswer(id); answerID != "" && of > 0 && len(a.answerParts(answerID)) >= of {
			continue // Ready
		}
		return id
	}
	return ""
}

// nearbyGroups314: the reported groups the server does not know itself.
// Member phones only for the Owner (the app's offline Get chat needs them).
func (a *API) nearbyGroups314(admin AdminAccount) []AdminNearbyGroup {
	a.ensureGroupNotes314()
	out := []AdminNearbyGroup{}
	rows, err := a.store.DB().Query(`SELECT n.gid, n.name, n.members_json, n.made_at, n.received_at FROM group_notes_314 n
		WHERE NOT EXISTS (SELECT 1 FROM groups g WHERE g.id = n.gid) ORDER BY n.name COLLATE NOCASE, n.gid LIMIT ?`, groupNotesMaxListed)
	if err != nil {
		return out
	}
	type raw struct{ id, name, mj, made, at string }
	list := []raw{}
	for rows.Next() {
		var x raw
		if rows.Scan(&x.id, &x.name, &x.mj, &x.made, &x.at) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	mine := a.adminDevices314(admin)
	for _, x := range list {
		devices := []string{}
		_ = json.Unmarshal([]byte(x.mj), &devices)
		g := AdminNearbyGroup{ID: x.id, Name: x.name, Members: len(devices), MadeAt: x.made, ReportedAt: x.at}
		for _, d := range devices {
			if mine[d] {
				g.Me = true
			}
		}
		if admin.Role == "owner" {
			g.Devices = devices
		}
		out = append(out, g)
	}
	return out
}

// groupMe314: which of these server groups the admin's user is a member of.
func (a *API) groupMe314(admin AdminAccount, groupIDs []string) map[string]bool {
	out := map[string]bool{}
	me := strings.TrimSpace(admin.UserID)
	if me == "" || len(groupIDs) == 0 {
		return out
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(groupIDs)), ",")
	args := []any{me}
	for _, id := range groupIDs {
		args = append(args, id)
	}
	rows, err := a.store.DB().Query(`SELECT DISTINCT gm.group_id FROM group_members gm JOIN devices d ON d.id = gm.device_id
		WHERE d.user_id = ? AND gm.group_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return out
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	rows.Close()
	return out
}

// groupDevicesMap314: member phones of these server groups (Owner only).
func (a *API) groupDevicesMap314(groupIDs []string) map[string][]string {
	out := map[string][]string{}
	if len(groupIDs) == 0 {
		return out
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(groupIDs)), ",")
	args := []any{}
	for _, id := range groupIDs {
		args = append(args, id)
	}
	rows, err := a.store.DB().Query(`SELECT group_id, device_id FROM group_members WHERE group_id IN (`+placeholders+`) ORDER BY device_id`, args...)
	if err != nil {
		return out
	}
	for rows.Next() {
		var g, d string
		if rows.Scan(&g, &d) == nil {
			out[g] = append(out[g], d)
		}
	}
	rows.Close()
	return out
}

// deviceIsAdmin314: the device belongs to the admin's own user.
func (a *API) deviceIsAdmin314(admin AdminAccount, deviceID string) bool {
	me := strings.TrimSpace(admin.UserID)
	if me == "" || deviceID == "" {
		return false
	}
	u, _ := a.deviceUser(deviceID)
	return u == me
}

func writeRefused314(w http.ResponseWriter, code, message string, extra map[string]any) {
	body := map[string]any{"ok": false, "error": message, "error_code": code, "code": code}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, http.StatusConflict, body)
}
