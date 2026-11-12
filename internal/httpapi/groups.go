package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxGroupMembers = 100

const maxGroupMessageBody = 16 * 1024

type Group struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OwnerID      string `json:"owner_id"`
	CreatedAt    string `json:"created_at"`
	Description  string `json:"description"`
	PhotoVersion int64  `json:"photo_version"`
	EditInfo     string `json:"edit_info"`
	SendMessages string `json:"send_messages"`
	AddMembers   string `json:"add_members"`
	CreatedBy    string `json:"created_by"`
}

type GroupMember struct {
	GroupID  string `json:"group_id"`
	DeviceID string `json:"device_id"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
	Name     string `json:"name,omitempty"`
	AddedBy  string `json:"added_by,omitempty"`
	UserID   string `json:"user_id,omitempty"`
}

type GroupMessage struct {
	ID          string       `json:"id"`
	GroupID     string       `json:"group_id"`
	SenderID    string       `json:"sender_id"`
	Body        string       `json:"body"`
	CreatedAt   string       `json:"created_at"`
	ServerSeq   int64        `json:"server_seq"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

func (a *API) ListGroups(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	params, err := parsePageParams(r, "groups:"+deviceID)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	args := []any{deviceID}
	where := `gm.device_id=?`
	if params.Cursor != nil {
		where += ` AND (g.name COLLATE NOCASE > ? OR (g.name COLLATE NOCASE=? AND g.id>?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT `+groupSelectCols+` FROM groups g JOIN group_members gm ON gm.group_id=g.id WHERE `+where+` ORDER BY g.name COLLATE NOCASE,g.id LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list groups")
		return
	}
	defer rows.Close()
	out := make([]Group, 0, params.Limit)
	for rows.Next() {
		var g Group
		if err := scanGroup(rows, &g); err != nil {
			writeError(w, 500, "failed to read groups")
			return
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read groups")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Groups []Group `json:"groups"`
		PageInfo
	}{Groups: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("groups:"+deviceID, last.Name, last.ID, false)
	}
	writeJSON(w, 200, resp)
}

func (a *API) CreateGroup(w http.ResponseWriter, r *http.Request) {
	owner, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	var in struct {
		Name         string   `json:"name"`
		MemberIDs    []string `json:"member_ids"`
		Description  string   `json:"description"`
		EditInfo     string   `json:"edit_info"`
		SendMessages string   `json:"send_messages"`
		AddMembers   string   `json:"add_members"`
		// Bundle G3: the id a phone made (Nearby groups keep it); bridge =
		// the group already exists on phones (no "created" line, people the
		// server never saw are left out instead of failing).
		ID     string `json:"id"`
		Bridge bool   `json:"bridge"`
	}
	if err := decodeJSON(r, &in, 32<<10); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 || hasControlChars(in.Name) {
		writeError(w, 400, "name must be 1-80 characters")
		return
	}
	// Bundle G2: optional description + settings (defaults as the mock).
	in.Description = strings.TrimSpace(in.Description)
	if len([]rune(in.Description)) > maxGroupDescription {
		writeError(w, 400, "description must be at most 300 characters")
		return
	}
	for _, s := range []*string{&in.EditInfo, &in.SendMessages, &in.AddMembers} {
		*s = strings.TrimSpace(*s)
	}
	if in.EditInfo == "" {
		in.EditInfo = "admins"
	}
	if in.SendMessages == "" {
		in.SendMessages = "all"
	}
	if in.AddMembers == "" {
		in.AddMembers = "admins"
	}
	if !validGroupSetting(in.EditInfo) || !validGroupSetting(in.SendMessages) || !validGroupSetting(in.AddMembers) {
		writeError(w, 400, "settings must be all or admins")
		return
	}
	ids := map[string]bool{owner: true}
	if len(in.MemberIDs) > maxGroupMembers-1 {
		writeError(w, 400, "group cannot exceed 100 members")
		return
	}
	for _, id := range in.MemberIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			ids[id] = true
		}
	}
	in.ID = strings.TrimSpace(in.ID)
	if in.ID != "" {
		if _, err := uuid.Parse(in.ID); err != nil || len(in.ID) != 36 {
			writeError(w, 400, "id must be a UUID")
			return
		}
		if existing, err := a.loadGroup(in.ID); err == nil {
			if existing.OwnerID != owner {
				writeError(w, 409, "group id already used")
				return
			}
			writeJSON(w, 200, existing)
			return
		}
	}
	for id := range ids {
		if !a.deviceExists(id) {
			if in.Bridge && id != owner {
				delete(ids, id)
				continue
			}
			writeError(w, 404, "member device not found: "+id)
			return
		}
	}
	gid := uuid.NewString()
	if in.ID != "" {
		gid = in.ID
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to create group")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO groups(id,name,owner_id,created_at,description,edit_info,send_messages,add_members,created_by) VALUES(?,?,?,?,?,?,?,?,?)`, gid, in.Name, owner, now, in.Description, in.EditInfo, in.SendMessages, in.AddMembers, owner); err != nil {
		writeError(w, 500, "failed to create group")
		return
	}
	for id := range ids {
		role := "member"
		if id == owner {
			role = "owner"
		}
		addedBy := owner
		if id == owner {
			addedBy = ""
		}
		if _, err = tx.Exec(`INSERT INTO group_members(group_id,device_id,role,joined_at,added_by) VALUES(?,?,?,?,?)`, gid, id, role, now, addedBy); err != nil {
			writeError(w, 500, "failed to add group member")
			return
		}
		if err = queueGroupUpsertTx(tx, id, gid); err != nil {
			writeError(w, 500, "failed to create group sync event")
			return
		}
	}
	// Bundle G2: "Sajjad created group" / "Sajjad added you" line.
	var added []string
	for id := range ids {
		if id != owner {
			added = append(added, id)
		}
	}
	sort.Strings(added)
	var ev GroupMessage
	var evTo []string
	if !in.Bridge {
		ev, evTo, err = a.emitGroupEventTx(tx, gid, groupEvent{Kind: "create", By: owner, Name: in.Name, Targets: added})
		if err != nil {
			writeError(w, 500, "failed to store group event")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to commit group")
		return
	}
	if !in.Bridge {
		a.sendGroupEvent(ev, evTo)
	}
	writeJSON(w, 201, Group{ID: gid, Name: in.Name, OwnerID: owner, CreatedAt: now, Description: in.Description, EditInfo: in.EditInfo, SendMessages: in.SendMessages, AddMembers: in.AddMembers, CreatedBy: owner})
}

func (a *API) GroupMembers(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	gid := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if gid == "" {
		writeError(w, 400, "group_id is required")
		return
	}
	if !a.isGroupMember(gid, deviceID) {
		writeError(w, 403, "device is not a group member")
		return
	}
	// Member display name must be the account profile nickname, not the
	// device label ("My Android Phone (xxxx)"): join through devices -> users
	// -> profiles and only fall back to the device name when no profile
	// nickname exists at all.
	rows, err := a.store.DB().Query(`SELECT gm.group_id,gm.device_id,gm.role,gm.joined_at,`+memberNameSQL+`,gm.added_by,COALESCE(d.user_id,'') FROM group_members gm JOIN devices d ON d.id=gm.device_id LEFT JOIN profiles p ON p.user_id=d.user_id WHERE gm.group_id=? ORDER BY gm.role DESC,`+memberNameSQL+` COLLATE NOCASE`, gid)
	if err != nil {
		writeError(w, 500, "failed to list members")
		return
	}
	defer rows.Close()
	out := []GroupMember{}
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.GroupID, &m.DeviceID, &m.Role, &m.JoinedAt, &m.Name, &m.AddedBy, &m.UserID); err != nil {
			writeError(w, 500, "failed to read members")
			return
		}
		out = append(out, m)
	}
	writeJSON(w, 200, out)
}

func (a *API) AddGroupMember(w http.ResponseWriter, r *http.Request) {
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
	// Bundle G2: admins add; everyone when "Add members" = All members.
	actorRole := a.groupRole(gid, actor)
	if actorRole == "" {
		writeGroupError(w, 403, groupErrNotMember, "device is not a group member")
		return
	}
	if !isAdminRole(actorRole) {
		if g, gerr := a.loadGroup(gid); gerr != nil || g.AddMembers != "all" {
			writeGroupError(w, 403, groupErrAdminsOnly, "only admins can add members")
			return
		}
	}
	var in struct {
		DeviceID string `json:"device_id"`
	}
	if err := decodeJSON(r, &in, 4096); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	in.DeviceID = strings.TrimSpace(in.DeviceID)
	if !a.deviceExists(in.DeviceID) {
		writeError(w, 404, "device not found")
		return
	}
	// Bundle G2 (B3): only your own friends can be added.
	// Bundle G6: or someone who asked to join with the invite link.
	if !a.isGroupMember(gid, in.DeviceID) && !a.areContacts(actor, in.DeviceID) && !a.hasJoinRequest(gid, in.DeviceID) {
		writeGroupError(w, 403, "GROUP_NOT_CONTACT", "you can only add your friends")
		return
	}
	var memberCount int
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM group_members WHERE group_id=?`, gid).Scan(&memberCount); err != nil {
		writeError(w, 500, "failed to count group members")
		return
	}
	if memberCount >= maxGroupMembers && !a.isGroupMember(gid, in.DeviceID) {
		writeError(w, 400, "group cannot exceed 100 members")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to add member")
		return
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT OR IGNORE INTO group_members(group_id,device_id,role,joined_at,added_by) VALUES(?,?,?,?,?)`, gid, in.DeviceID, "member", now, actor)
	if err != nil {
		writeError(w, 500, "failed to add member")
		return
	}
	_, _ = tx.Exec(`DELETE FROM group_join_requests WHERE group_id=? AND device_id=?`, gid, in.DeviceID)
	affected, err := result.RowsAffected()
	if err != nil {
		writeError(w, 500, "failed to determine membership change")
		return
	}
	// Rebuild the latest membership event for every remaining member so their
	// offline roster is updated too.
	members, err := a.groupMemberIDsTx(tx, gid)
	if err != nil {
		writeError(w, 500, "failed to read group members")
		return
	}
	for _, memberID := range members {
		if err = queueGroupUpsertTx(tx, memberID, gid); err != nil {
			writeError(w, 500, "failed to create group sync event")
			return
		}
	}
	var ev GroupMessage
	var evTo []string
	if affected == 1 {
		ev, evTo, err = a.emitGroupEventTx(tx, gid, groupEvent{Kind: "add", By: actor, Targets: []string{in.DeviceID}})
		if err != nil {
			writeError(w, 500, "failed to store group event")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to add member")
		return
	}
	a.sendGroupEvent(ev, evTo)
	status := http.StatusOK
	if affected == 1 {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"ok": true, "changed": affected == 1})
}

func (a *API) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
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
	actorRole := a.groupRole(gid, actor)
	if actorRole == "" {
		writeGroupError(w, 403, groupErrNotMember, "device is not a group member")
		return
	}
	if !isAdminRole(actorRole) {
		writeGroupError(w, 403, groupErrAdminsOnly, "only admins can remove members")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("device_id"))
	if id == "" {
		writeError(w, 400, "device_id is required")
		return
	}
	if id == actor {
		writeError(w, 400, "owner cannot remove itself")
		return
	}
	// Bundle G2: nobody removes the owner; only the owner removes an admin.
	switch a.groupRole(gid, id) {
	case "owner":
		writeGroupError(w, 403, groupErrAdminsOnly, "the owner cannot be removed")
		return
	case "admin":
		if actorRole != "owner" {
			writeGroupError(w, 403, groupErrAdminsOnly, "only the owner can remove an admin")
			return
		}
	}
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to remove member")
		return
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM group_members WHERE group_id=? AND device_id=?`, gid, id)
	if err != nil {
		writeError(w, 500, "failed to remove member")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeError(w, 500, "failed to determine membership change")
		return
	}
	if affected == 0 {
		writeError(w, http.StatusNotFound, "device is not a group member")
		return
	}
	// Remove stale membership/message events for this device and keep only the
	// latest removal event. This prevents a later re-add from being masked by
	// an older group_removed event when the device syncs after being offline.
	if _, err = tx.Exec(`DELETE FROM sync_events WHERE device_id=? AND event_type='group_message' AND event_id IN (SELECT id FROM group_messages WHERE group_id=?)`, id, gid); err != nil {
		writeError(w, 500, "failed to clean pending group message events")
		return
	}
	if err = queueGroupRemovedTx(tx, id, gid); err != nil {
		writeError(w, 500, "failed to create group removal event")
		return
	}
	// Bundle G2: "Sajjad removed Sara" - the removed phone gets it live too.
	ev, evTo, err := a.emitGroupEventTx(tx, gid, groupEvent{Kind: "remove", By: actor, Targets: []string{id}})
	if err != nil {
		writeError(w, 500, "failed to store group event")
		return
	}
	evTo = append(evTo, id)
	remaining, err := a.groupMemberIDsTx(tx, gid)
	if err != nil {
		writeError(w, 500, "failed to read remaining group members")
		return
	}
	for _, memberID := range remaining {
		if err = queueGroupUpsertTx(tx, memberID, gid); err != nil {
			writeError(w, 500, "failed to update group roster")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to remove member")
		return
	}
	a.sendGroupEvent(ev, evTo)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) ListGroupMessages(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	gid := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if gid == "" {
		writeError(w, 400, "group_id is required")
		return
	}
	if !a.isGroupMember(gid, deviceID) {
		writeError(w, 403, "device is not a group member")
		return
	}
	params, err := parsePageParams(r, "group-messages:"+gid)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	args := []any{gid}
	where := `group_id=?`
	if params.Cursor != nil {
		where += ` AND (server_seq > ? OR (server_seq=? AND id>?))`
		seq := params.Cursor.Key
		args = append(args, seq, seq, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT id,group_id,sender_id,body,created_at,server_seq FROM group_messages WHERE `+where+` ORDER BY server_seq ASC,id ASC LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list group messages")
		return
	}
	defer rows.Close()
	out := make([]GroupMessage, 0, params.Limit)
	ids := make([]string, 0, params.Limit)
	for rows.Next() {
		var m GroupMessage
		if err := rows.Scan(&m.ID, &m.GroupID, &m.SenderID, &m.Body, &m.CreatedAt, &m.ServerSeq); err != nil {
			writeError(w, 500, "failed to read group messages")
			return
		}
		out = append(out, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read group messages")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
		ids = ids[:params.Limit]
	}
	attachmentMap, err := a.attachmentsForGroupMessages(ids)
	if err != nil {
		writeError(w, 500, "failed to read group attachments")
		return
	}
	for i := range out {
		out[i].Attachments = attachmentMap[out[i].ID]
	}
	resp := struct {
		GroupMessages []GroupMessage `json:"group_messages"`
		PageInfo
	}{GroupMessages: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("group-messages:"+gid, strconv.FormatInt(last.ServerSeq, 10), last.ID, false)
	}
	writeJSON(w, 200, resp)
}

func (a *API) CreateGroupMessage(w http.ResponseWriter, r *http.Request) {
	sender, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	var in struct {
		ID            string   `json:"id"`
		GroupID       string   `json:"group_id"`
		Body          string   `json:"body"`
		CreatedAt     string   `json:"created_at"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if err := decodeJSON(r, &in, maxGroupMessageBody+16<<10); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	in.ID = strings.TrimSpace(in.ID)
	in.GroupID = strings.TrimSpace(in.GroupID)
	in.CreatedAt = strings.TrimSpace(in.CreatedAt)
	createdAt, ok := normalizeGroupMessageCreatedAt(in.CreatedAt)
	if in.GroupID == "" || !ok || (strings.TrimSpace(in.Body) == "" && len(in.AttachmentIDs) == 0) || len(in.Body) > maxGroupMessageBody || len(in.AttachmentIDs) > maxAttachmentsPerMessage {
		writeError(w, 400, "group_id, created_at and body or attachment are required; maximum body is 16KB")
		return
	}
	if !a.isGroupMember(in.GroupID, sender) {
		writeGroupError(w, 403, groupErrNotMember, "device is not a group member")
		return
	}
	if !a.allowMessage(sender) {
		writeError(w, 429, "message rate limit exceeded")
		return
	}
	m, existing, err := a.insertGroupMessage(sender, in.GroupID, in.Body, createdAt, in.ID, in.AttachmentIDs)
	if err != nil {
		errorText := err.Error()
		if errors.Is(err, errGroupAdminsOnly) {
			writeGroupError(w, http.StatusForbidden, groupErrAdminsOnly, errorText)
			return
		}
		if strings.Contains(errorText, "device is not a group member") {
			writeGroupError(w, http.StatusForbidden, groupErrNotMember, errorText)
			return
		}
		if strings.Contains(errorText, "different message") || strings.Contains(errorText, "attachment") || strings.Contains(errorText, "too many") {
			writeError(w, http.StatusConflict, errorText)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to store group message")
		return
	}
	if !existing {
		a.broadcastGroupMessage(m)
	}
	status := 201
	if existing {
		status = 200
	}
	writeJSON(w, status, m)
}

func normalizeGroupMessageCreatedAt(value string) (string, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	return parsed.UTC().Format(time.RFC3339Nano), true
}

func (a *API) insertGroupMessage(sender, gid, body, createdAt, requested string, attachmentIDs []string) (GroupMessage, bool, error) {
	body = strings.TrimSpace(body)
	if len(body) > maxGroupMessageBody || (body == "" && len(attachmentIDs) == 0) {
		return GroupMessage{}, false, errors.New("group message body or attachment is required")
	}
	normalizedCreatedAt, ok := normalizeGroupMessageCreatedAt(createdAt)
	if !ok {
		return GroupMessage{}, false, errors.New("invalid group message created_at")
	}
	createdAt = normalizedCreatedAt
	id := requested
	if id == "" {
		id = uuid.NewString()
	}
	tx, err := a.store.DB().Begin()
	if err != nil {
		return GroupMessage{}, false, err
	}
	defer tx.Rollback()
	var m GroupMessage
	err = tx.QueryRow(`SELECT id,group_id,sender_id,body,created_at,server_seq FROM group_messages WHERE id=?`, id).Scan(&m.ID, &m.GroupID, &m.SenderID, &m.Body, &m.CreatedAt, &m.ServerSeq)
	if err == nil {
		if m.GroupID != gid || m.SenderID != sender || m.Body != body || m.CreatedAt != createdAt {
			return m, true, errors.New("group message id already belongs to different message")
		}
		if len(attachmentIDs) > 0 {
			existingIDs, err := a.attachmentIDsTx(tx, id, true)
			if err != nil {
				return m, true, err
			}
			if len(existingIDs) == 0 {
				if err := a.validateAndAttachFiles(tx, id, true, sender, attachmentIDs); err != nil {
					return m, true, err
				}
			} else if !sameStringSlice(existingIDs, normalizeIDs(attachmentIDs)) {
				return m, true, errors.New("group message id already belongs to a different attachment set")
			}
		}
		if err := tx.Commit(); err != nil {
			return GroupMessage{}, false, err
		}
		m.Attachments, err = a.attachmentsForGroupMessage(id)
		return m, true, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return m, false, err
	}
	var senderRole string
	if err = tx.QueryRow(`SELECT role FROM group_members WHERE group_id=? AND device_id=?`, gid, sender).Scan(&senderRole); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return m, false, errors.New("device is not a group member")
		}
		return m, false, err
	}
	// Bundle G2: "Send messages: Only admins". System lines are not
	// written here; they never get this check.
	if !isAdminRole(senderRole) && !strings.HasPrefix(body, groupEventPrefix) {
		var send string
		if err = tx.QueryRow(`SELECT send_messages FROM groups WHERE id=?`, gid).Scan(&send); err != nil {
			return m, false, err
		}
		if send == "admins" {
			return m, false, errGroupAdminsOnly
		}
	}
	// Bundle G2: only the server writes system lines.
	if strings.HasPrefix(body, groupEventPrefix) {
		return m, false, errors.New("group message body is reserved")
	}
	if len(attachmentIDs) > maxAttachmentsPerMessage {
		return m, false, errors.New("too many attachments")
	}
	var seq int64
	if err = tx.QueryRow(`SELECT COALESCE(MAX(server_seq),0)+1 FROM group_messages WHERE group_id=?`, gid).Scan(&seq); err != nil {
		return m, false, err
	}
	if _, err = tx.Exec(`INSERT INTO group_messages(id,group_id,sender_id,body,created_at,server_seq) VALUES(?,?,?,?,?,?)`, id, gid, sender, body, createdAt, seq); err != nil {
		return m, false, err
	}
	if err = a.validateAndAttachFiles(tx, id, true, sender, attachmentIDs); err != nil {
		return m, false, err
	}
	m = GroupMessage{ID: id, GroupID: gid, SenderID: sender, Body: body, CreatedAt: createdAt, ServerSeq: seq}
	members, err := a.groupMemberIDsTx(tx, gid)
	if err != nil {
		return m, false, err
	}
	for _, memberID := range members {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, memberID, "group_message", id); err != nil {
			return m, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return m, false, err
	}
	m.Attachments, err = a.attachmentsForGroupMessage(id)
	if err != nil {
		return GroupMessage{}, false, err
	}
	return m, false, nil
}

func (a *API) broadcastGroupMessage(m GroupMessage) {
	members, _ := a.groupMemberIDs(m.GroupID)
	payload := mustJSON(WSMessage{Type: "group_message", ID: m.ID, GroupID: m.GroupID, SenderID: m.SenderID, Body: m.Body, CreatedAt: m.CreatedAt, ServerSeq: m.ServerSeq, Attachments: m.Attachments})
	for _, id := range members {
		if id != m.SenderID {
			a.hub.SendTo(id, payload)
		}
	}
}
func (a *API) groupMemberIDs(gid string) ([]string, error) {
	rows, err := a.store.DB().Query(`SELECT device_id FROM group_members WHERE group_id=?`, gid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (a *API) isGroupMember(gid, id string) bool {
	var n int
	return a.store.DB().QueryRow(`SELECT 1 FROM group_members WHERE group_id=? AND device_id=?`, gid, id).Scan(&n) == nil
}
func (a *API) isGroupOwner(gid, id string) bool {
	var n int
	return a.store.DB().QueryRow(`SELECT 1 FROM groups WHERE id=? AND owner_id=?`, gid, id).Scan(&n) == nil
}
func (a *API) groupMemberIDsTx(tx *sql.Tx, gid string) ([]string, error) {
	rows, err := tx.Query(`SELECT device_id FROM group_members WHERE group_id=?`, gid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func queueGroupUpsertTx(tx *sql.Tx, deviceID, groupID string) error {
	if _, err := tx.Exec(`DELETE FROM sync_events WHERE device_id=? AND event_type IN ('group_upsert','group_removed') AND event_id=?`, deviceID, groupID); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, deviceID, "group_upsert", groupID)
	return err
}

func queueGroupRemovedTx(tx *sql.Tx, deviceID, groupID string) error {
	if _, err := tx.Exec(`DELETE FROM sync_events WHERE device_id=? AND event_type IN ('group_upsert','group_removed') AND event_id=?`, deviceID, groupID); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, deviceID, "group_removed", groupID)
	return err
}
