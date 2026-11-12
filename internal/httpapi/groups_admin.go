package httpapi

// Bundle G2: group admins, info, photo, leave and system events.
//
// Rules (TODO_groups.md §3/§8):
//   - roles: "owner" (one), "admin", "member".
//   - settings: edit_info / send_messages / add_members = "all" | "admins".
//     Defaults: edit_info=admins, send_messages=all, add_members=admins.
//   - only admins change settings; admins make admins; only the owner
//     dismisses an admin or removes an admin.
//   - leaving owner hands over to the oldest admin, else the oldest member;
//     the last member leaving deletes the group.
//   - every change is also a normal group message with the body
//     "llev:v1:{json}" so all phones (live or later) show a system line.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const groupEventPrefix = "llev:v1:"

const maxGroupPhotoBytes = 256 << 10

const maxGroupDescription = 300

// Error codes the app reads (WebSocket "error_code" / HTTP "error_code").
const (
	groupErrNotMember  = "GROUP_NOT_MEMBER"
	groupErrAdminsOnly = "GROUP_ADMINS_ONLY"
)

var errGroupAdminsOnly = errors.New("only admins can send messages")

const groupSelectCols = `g.id,g.name,g.owner_id,g.created_at,g.description,g.photo_version,g.edit_info,g.send_messages,g.add_members,g.created_by`

func scanGroup(row interface{ Scan(...any) error }, g *Group) error {
	return row.Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt, &g.Description, &g.PhotoVersion, &g.EditInfo, &g.SendMessages, &g.AddMembers, &g.CreatedBy)
}

func (a *API) loadGroup(gid string) (Group, error) {
	var g Group
	err := scanGroup(a.store.DB().QueryRow(`SELECT `+groupSelectCols+` FROM groups g WHERE g.id=?`, gid), &g)
	return g, err
}

// memberNameSQL: profile nickname first, then the username (Bundle 276),
// then a phone name a person typed; never "My Android Phone (xxxx)".
var memberNameSQL = `(CASE WHEN ` + okNameSQL276("p.display_name") + ` THEN TRIM(p.display_name)
	WHEN TRIM(COALESCE((SELECT uu.username FROM users uu WHERE uu.id=d.user_id),''))<>'' THEN (SELECT uu.username FROM users uu WHERE uu.id=d.user_id)
	WHEN ` + okNameSQL276("d.name") + ` THEN TRIM(d.name) ELSE 'LinkUp user' END)`

func (a *API) groupRole(gid, id string) string {
	var role string
	if err := a.store.DB().QueryRow(`SELECT role FROM group_members WHERE group_id=? AND device_id=?`, gid, id).Scan(&role); err != nil {
		return ""
	}
	return role
}

func isAdminRole(role string) bool { return role == "owner" || role == "admin" }

func groupRoleTx(tx *sql.Tx, gid, id string) string {
	var role string
	if err := tx.QueryRow(`SELECT role FROM group_members WHERE group_id=? AND device_id=?`, gid, id).Scan(&role); err != nil {
		return ""
	}
	return role
}

func validGroupSetting(v string) bool { return v == "all" || v == "admins" }

func writeGroupError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": message, "error_code": code})
}

// deviceNames returns profile names for the ids (used inside events so a
// phone can name someone who already left).
func (a *API) deviceNamesTx(tx *sql.Tx, ids []string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		var name string
		if err := tx.QueryRow(`SELECT `+memberNameSQL+` FROM devices d LEFT JOIN profiles p ON p.user_id=d.user_id WHERE d.id=?`, id).Scan(&name); err == nil {
			out[id] = strings.TrimSpace(name)
		}
	}
	return out
}

type groupEvent struct {
	Kind    string            `json:"k"`
	By      string            `json:"by"`
	Targets []string          `json:"t,omitempty"`
	Name    string            `json:"n,omitempty"`
	Field   string            `json:"f,omitempty"`
	Value   string            `json:"v,omitempty"`
	Owner   string            `json:"o,omitempty"`
	Names   map[string]string `json:"nm,omitempty"`
}

// emitGroupEventTx stores the event as a group message from [ev.By] and
// queues it for every current member plus [extra] (e.g. the removed one).
// Returns the message so the caller can broadcast it after commit.
func (a *API) emitGroupEventTx(tx *sql.Tx, gid string, ev groupEvent, extra ...string) (GroupMessage, []string, error) {
	ids := append([]string{ev.By, ev.Owner}, ev.Targets...)
	ev.Names = a.deviceNamesTx(tx, ids)
	raw, err := json.Marshal(ev)
	if err != nil {
		return GroupMessage{}, nil, err
	}
	body := groupEventPrefix + string(raw)
	var seq int64
	if err = tx.QueryRow(`SELECT COALESCE(MAX(server_seq),0)+1 FROM group_messages WHERE group_id=?`, gid).Scan(&seq); err != nil {
		return GroupMessage{}, nil, err
	}
	id := "ev-" + uuid.NewString()
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.Exec(`INSERT INTO group_messages(id,group_id,sender_id,body,created_at,server_seq) VALUES(?,?,?,?,?,?)`, id, gid, ev.By, body, createdAt, seq); err != nil {
		return GroupMessage{}, nil, err
	}
	members, err := a.groupMemberIDsTx(tx, gid)
	if err != nil {
		return GroupMessage{}, nil, err
	}
	seen := map[string]bool{}
	var to []string
	for _, m := range append(members, extra...) {
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		to = append(to, m)
		if _, err = tx.Exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, m, "group_message", id); err != nil {
			return GroupMessage{}, nil, err
		}
	}
	return GroupMessage{ID: id, GroupID: gid, SenderID: ev.By, Body: body, CreatedAt: createdAt, ServerSeq: seq}, to, nil
}

// sendGroupEvent pushes a stored event to every phone in [to], the actor
// too (the actor did not send it from its own outbox).
func (a *API) sendGroupEvent(m GroupMessage, to []string) {
	if m.ID == "" {
		return
	}
	payload := mustJSON(WSMessage{Type: "group_message", ID: m.ID, GroupID: m.GroupID, SenderID: m.SenderID, Body: m.Body, CreatedAt: m.CreatedAt, ServerSeq: m.ServerSeq})
	for _, id := range to {
		a.hub.SendTo(id, payload)
	}
}

func queueRosterTx(a *API, tx *sql.Tx, gid string) error {
	members, err := a.groupMemberIDsTx(tx, gid)
	if err != nil {
		return err
	}
	for _, id := range members {
		if err := queueGroupUpsertTx(tx, id, gid); err != nil {
			return err
		}
	}
	return nil
}

// areContacts: the two devices belong to accounts that are contacts. A
// device without an account (old installs) is not checked.
func (a *API) areContacts(deviceA, deviceB string) bool {
	var userA, userB sql.NullString
	_ = a.store.DB().QueryRow(`SELECT user_id FROM devices WHERE id=?`, deviceA).Scan(&userA)
	_ = a.store.DB().QueryRow(`SELECT user_id FROM devices WHERE id=?`, deviceB).Scan(&userB)
	if !userA.Valid || !userB.Valid || userA.String == "" || userB.String == "" {
		return true
	}
	if userA.String == userB.String {
		return true
	}
	var n int
	return a.store.DB().QueryRow(`SELECT 1 FROM account_contacts WHERE (user_id=? AND contact_user_id=?) OR (user_id=? AND contact_user_id=?) LIMIT 1`, userA.String, userB.String, userB.String, userA.String).Scan(&n) == nil
}

// GroupInfo: GET /api/v1/groups/info?group_id= (members only).
func (a *API) GroupInfo(w http.ResponseWriter, r *http.Request) {
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
	if !a.isGroupMember(gid, actor) {
		writeGroupError(w, 403, groupErrNotMember, "device is not a group member")
		return
	}
	g, err := a.loadGroup(gid)
	if err != nil {
		writeError(w, 404, "group not found")
		return
	}
	writeJSON(w, 200, g)
}

// UpdateGroup: PATCH /api/v1/groups?group_id= {name?, description?,
// edit_info?, send_messages?, add_members?}.
func (a *API) UpdateGroup(w http.ResponseWriter, r *http.Request) {
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
	var in struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		EditInfo     *string `json:"edit_info"`
		SendMessages *string `json:"send_messages"`
		AddMembers   *string `json:"add_members"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	role := a.groupRole(gid, actor)
	if role == "" {
		writeGroupError(w, 403, groupErrNotMember, "device is not a group member")
		return
	}
	g, err := a.loadGroup(gid)
	if err != nil {
		writeError(w, 404, "group not found")
		return
	}
	admin := isAdminRole(role)
	editInfo := in.Name != nil || in.Description != nil
	if editInfo && !admin && g.EditInfo != "all" {
		writeGroupError(w, 403, groupErrAdminsOnly, "only admins can edit group info")
		return
	}
	settings := in.EditInfo != nil || in.SendMessages != nil || in.AddMembers != nil
	if settings && !admin {
		writeGroupError(w, 403, groupErrAdminsOnly, "only admins can change group settings")
		return
	}
	var events []groupEvent
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" || len(name) > 80 || hasControlChars(name) {
			writeError(w, 400, "name must be 1-80 characters")
			return
		}
		if name != g.Name {
			g.Name = name
			events = append(events, groupEvent{Kind: "name", By: actor, Name: name})
		}
	}
	if in.Description != nil {
		desc := strings.TrimSpace(*in.Description)
		if len([]rune(desc)) > maxGroupDescription {
			writeError(w, 400, "description must be at most 300 characters")
			return
		}
		if desc != g.Description {
			g.Description = desc
			events = append(events, groupEvent{Kind: "desc", By: actor})
		}
	}
	for _, s := range []struct {
		field string
		in    *string
		cur   *string
	}{{"edit_info", in.EditInfo, &g.EditInfo}, {"send_messages", in.SendMessages, &g.SendMessages}, {"add_members", in.AddMembers, &g.AddMembers}} {
		if s.in == nil {
			continue
		}
		v := strings.TrimSpace(*s.in)
		if !validGroupSetting(v) {
			writeError(w, 400, s.field+" must be all or admins")
			return
		}
		if v != *s.cur {
			*s.cur = v
			events = append(events, groupEvent{Kind: "set", By: actor, Field: s.field, Value: v})
		}
	}
	if len(events) == 0 {
		writeJSON(w, 200, g)
		return
	}
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to update group")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE groups SET name=?,description=?,edit_info=?,send_messages=?,add_members=? WHERE id=?`, g.Name, g.Description, g.EditInfo, g.SendMessages, g.AddMembers, gid); err != nil {
		writeError(w, 500, "failed to update group")
		return
	}
	type sent struct {
		m  GroupMessage
		to []string
	}
	var out []sent
	for _, ev := range events {
		m, to, err := a.emitGroupEventTx(tx, gid, ev)
		if err != nil {
			writeError(w, 500, "failed to store group event")
			return
		}
		out = append(out, sent{m, to})
	}
	if err = queueRosterTx(a, tx, gid); err != nil {
		writeError(w, 500, "failed to update group roster")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to update group")
		return
	}
	for _, s := range out {
		a.sendGroupEvent(s.m, s.to)
	}
	writeJSON(w, 200, g)
}

// GroupPhoto: GET /api/v1/groups/photo?group_id= (members only).
func (a *API) GroupPhoto(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	gid := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if gid == "" || !a.isGroupMember(gid, actor) {
		writeGroupError(w, 403, groupErrNotMember, "device is not a group member")
		return
	}
	var data []byte
	var version int64
	err := a.store.DB().QueryRow(`SELECT gp.jpeg,g.photo_version FROM group_photos gp JOIN groups g ON g.id=gp.group_id WHERE gp.group_id=?`, gid).Scan(&data, &version)
	if err != nil {
		writeError(w, 404, "no group photo")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("ETag", strconv.FormatInt(version, 10))
	w.Header().Set("Cache-Control", "private, max-age=0")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

// SetGroupPhoto: PUT /api/v1/groups/photo?group_id= with a JPEG body
// (max 256 KB). DELETE removes it.
func (a *API) SetGroupPhoto(w http.ResponseWriter, r *http.Request) {
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
	g, err := a.loadGroup(gid)
	if err != nil {
		writeError(w, 404, "group not found")
		return
	}
	if !isAdminRole(role) && g.EditInfo != "all" {
		writeGroupError(w, 403, groupErrAdminsOnly, "only admins can edit group info")
		return
	}
	remove := r.Method == http.MethodDelete
	var data []byte
	if !remove {
		data, err = io.ReadAll(io.LimitReader(r.Body, maxGroupPhotoBytes+1))
		if err != nil {
			writeError(w, 400, "could not read photo")
			return
		}
		if len(data) > maxGroupPhotoBytes {
			writeError(w, 413, "photo must be at most 256 KB")
			return
		}
		if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
			writeError(w, 400, "photo must be a JPEG")
			return
		}
	}
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to save photo")
		return
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	kind := "photo"
	if remove {
		kind = "photo_removed"
		if _, err = tx.Exec(`DELETE FROM group_photos WHERE group_id=?`, gid); err != nil {
			writeError(w, 500, "failed to remove photo")
			return
		}
	} else if _, err = tx.Exec(`INSERT INTO group_photos(group_id,jpeg,updated_at) VALUES(?,?,?) ON CONFLICT(group_id) DO UPDATE SET jpeg=excluded.jpeg,updated_at=excluded.updated_at`, gid, data, now); err != nil {
		writeError(w, 500, "failed to save photo")
		return
	}
	if _, err = tx.Exec(`UPDATE groups SET photo_version=photo_version+1 WHERE id=?`, gid); err != nil {
		writeError(w, 500, "failed to save photo")
		return
	}
	var version int64
	if err = tx.QueryRow(`SELECT photo_version FROM groups WHERE id=?`, gid).Scan(&version); err != nil {
		writeError(w, 500, "failed to save photo")
		return
	}
	m, to, err := a.emitGroupEventTx(tx, gid, groupEvent{Kind: kind, By: actor, Value: strconv.FormatInt(version, 10)})
	if err != nil {
		writeError(w, 500, "failed to store group event")
		return
	}
	if err = queueRosterTx(a, tx, gid); err != nil {
		writeError(w, 500, "failed to update group roster")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to save photo")
		return
	}
	a.sendGroupEvent(m, to)
	writeJSON(w, 200, map[string]any{"ok": true, "photo_version": version})
}

// LeaveGroup: POST /api/v1/groups/leave?group_id=.
func (a *API) LeaveGroup(w http.ResponseWriter, r *http.Request) {
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
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to leave group")
		return
	}
	defer tx.Rollback()
	role := groupRoleTx(tx, gid, actor)
	if role == "" {
		// Already out: leaving twice is fine.
		writeJSON(w, 200, map[string]any{"ok": true, "changed": false})
		return
	}
	var others []string
	rows, err := tx.Query(`SELECT device_id FROM group_members WHERE group_id=? AND device_id<>? ORDER BY CASE role WHEN 'admin' THEN 0 ELSE 1 END, joined_at, device_id`, gid, actor)
	if err != nil {
		writeError(w, 500, "failed to leave group")
		return
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			writeError(w, 500, "failed to leave group")
			return
		}
		others = append(others, id)
	}
	rows.Close()
	if len(others) == 0 {
		// Last one out: the group is gone.
		if _, err = tx.Exec(`DELETE FROM sync_events WHERE event_type='group_message' AND event_id IN (SELECT id FROM group_messages WHERE group_id=?)`, gid); err != nil {
			writeError(w, 500, "failed to delete group")
			return
		}
		if err = queueGroupRemovedTx(tx, actor, gid); err != nil {
			writeError(w, 500, "failed to delete group")
			return
		}
		if _, err = tx.Exec(`DELETE FROM group_message_attachments WHERE group_message_id IN (SELECT id FROM group_messages WHERE group_id=?)`, gid); err != nil {
			writeError(w, 500, "failed to delete group")
			return
		}
		for _, q := range []string{`DELETE FROM group_photos WHERE group_id=?`, `DELETE FROM group_messages WHERE group_id=?`, `DELETE FROM group_members WHERE group_id=?`, `DELETE FROM groups WHERE id=?`} {
			if _, err = tx.Exec(q, gid); err != nil {
				writeError(w, 500, "failed to delete group")
				return
			}
		}
		if err = tx.Commit(); err != nil {
			writeError(w, 500, "failed to delete group")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "changed": true, "deleted": true})
		return
	}
	newOwner := ""
	if role == "owner" {
		newOwner = others[0]
		if _, err = tx.Exec(`UPDATE groups SET owner_id=? WHERE id=?`, newOwner, gid); err != nil {
			writeError(w, 500, "failed to hand over the group")
			return
		}
		if _, err = tx.Exec(`UPDATE group_members SET role='owner' WHERE group_id=? AND device_id=?`, gid, newOwner); err != nil {
			writeError(w, 500, "failed to hand over the group")
			return
		}
	}
	m, _, err := a.emitGroupEventTx(tx, gid, groupEvent{Kind: "leave", By: actor, Owner: newOwner})
	if err != nil {
		writeError(w, 500, "failed to store group event")
		return
	}
	if _, err = tx.Exec(`DELETE FROM group_members WHERE group_id=? AND device_id=?`, gid, actor); err != nil {
		writeError(w, 500, "failed to leave group")
		return
	}
	if _, err = tx.Exec(`DELETE FROM sync_events WHERE device_id=? AND event_type='group_message' AND event_id IN (SELECT id FROM group_messages WHERE group_id=?)`, actor, gid); err != nil {
		writeError(w, 500, "failed to leave group")
		return
	}
	if err = queueGroupRemovedTx(tx, actor, gid); err != nil {
		writeError(w, 500, "failed to leave group")
		return
	}
	if err = queueRosterTx(a, tx, gid); err != nil {
		writeError(w, 500, "failed to update group roster")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to leave group")
		return
	}
	a.sendGroupEvent(m, append(others, actor))
	writeJSON(w, 200, map[string]any{"ok": true, "changed": true, "owner_id": newOwner})
}

// SetGroupRole: POST /api/v1/groups/role?group_id= {device_id, role}.
// role "admin" = make admin (any admin); "member" = dismiss (owner only).
func (a *API) SetGroupRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	gid := strings.TrimSpace(r.URL.Query().Get("group_id"))
	var in struct {
		DeviceID string `json:"device_id"`
		Role     string `json:"role"`
	}
	if err := decodeJSON(r, &in, 4096); err != nil || gid == "" {
		writeError(w, 400, "group_id, device_id and role are required")
		return
	}
	in.DeviceID = strings.TrimSpace(in.DeviceID)
	in.Role = strings.TrimSpace(in.Role)
	if in.Role != "admin" && in.Role != "member" {
		writeError(w, 400, "role must be admin or member")
		return
	}
	actorRole := a.groupRole(gid, actor)
	if actorRole == "" {
		writeGroupError(w, 403, groupErrNotMember, "device is not a group member")
		return
	}
	if !isAdminRole(actorRole) {
		writeGroupError(w, 403, groupErrAdminsOnly, "only admins can change admins")
		return
	}
	target := a.groupRole(gid, in.DeviceID)
	if target == "" {
		writeError(w, 404, "device is not a group member")
		return
	}
	if target == "owner" {
		writeError(w, 400, "the owner's role cannot change")
		return
	}
	if in.Role == "member" && actorRole != "owner" {
		writeGroupError(w, 403, groupErrAdminsOnly, "only the owner can dismiss an admin")
		return
	}
	if target == in.Role {
		writeJSON(w, 200, map[string]any{"ok": true, "changed": false})
		return
	}
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to change role")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE group_members SET role=? WHERE group_id=? AND device_id=?`, in.Role, gid, in.DeviceID); err != nil {
		writeError(w, 500, "failed to change role")
		return
	}
	kind := "admin"
	if in.Role == "member" {
		kind = "unadmin"
	}
	m, to, err := a.emitGroupEventTx(tx, gid, groupEvent{Kind: kind, By: actor, Targets: []string{in.DeviceID}})
	if err != nil {
		writeError(w, 500, "failed to store group event")
		return
	}
	if err = queueRosterTx(a, tx, gid); err != nil {
		writeError(w, 500, "failed to update group roster")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to change role")
		return
	}
	a.sendGroupEvent(m, to)
	writeJSON(w, 200, map[string]any{"ok": true, "changed": true})
}
