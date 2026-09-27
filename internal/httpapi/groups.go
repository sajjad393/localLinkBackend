package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxGroupMembers = 100

const maxEncryptedGroupMessageBody = 64 * 1024

type Group struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OwnerID       string `json:"owner_id"`
	CreatedAt     string `json:"created_at"`
	KeyVersion    int    `json:"key_version"`
	RekeyRequired bool   `json:"rekey_required"`
}

type GroupMember struct {
	GroupID  string `json:"group_id"`
	DeviceID string `json:"device_id"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
	Name     string `json:"name,omitempty"`
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
	rows, err := a.store.DB().Query(`SELECT g.id,g.name,g.owner_id,g.created_at,g.key_version,g.rekey_required FROM groups g JOIN group_members gm ON gm.group_id=g.id WHERE `+where+` ORDER BY g.name COLLATE NOCASE,g.id LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list groups")
		return
	}
	defer rows.Close()
	out := make([]Group, 0, params.Limit)
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt, &g.KeyVersion, &g.RekeyRequired); err != nil {
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
		Name      string   `json:"name"`
		MemberIDs []string `json:"member_ids"`
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
	for id := range ids {
		if !a.deviceExists(id) {
			writeError(w, 404, "member device not found: "+id)
			return
		}
	}
	gid := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, 500, "failed to create group")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO groups(id,name,owner_id,created_at,key_version,rekey_required) VALUES(?,?,?,?,?,?)`, gid, in.Name, owner, now, 1, 1); err != nil {
		writeError(w, 500, "failed to create group")
		return
	}
	for id := range ids {
		role := "member"
		if id == owner {
			role = "owner"
		}
		if _, err = tx.Exec(`INSERT INTO group_members(group_id,device_id,role,joined_at) VALUES(?,?,?,?)`, gid, id, role, now); err != nil {
			writeError(w, 500, "failed to add group member")
			return
		}
		if err = queueGroupUpsertTx(tx, id, gid); err != nil {
			writeError(w, 500, "failed to create group sync event")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to commit group")
		return
	}
	writeJSON(w, 201, Group{ID: gid, Name: in.Name, OwnerID: owner, CreatedAt: now, KeyVersion: 1, RekeyRequired: true})
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
	rows, err := a.store.DB().Query(`SELECT gm.group_id,gm.device_id,gm.role,gm.joined_at,d.name FROM group_members gm JOIN devices d ON d.id=gm.device_id WHERE gm.group_id=? ORDER BY gm.role DESC,d.name COLLATE NOCASE`, gid)
	if err != nil {
		writeError(w, 500, "failed to list members")
		return
	}
	defer rows.Close()
	out := []GroupMember{}
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.GroupID, &m.DeviceID, &m.Role, &m.JoinedAt, &m.Name); err != nil {
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
	if !a.isGroupOwner(gid, actor) {
		writeError(w, 403, "only the group owner can add members")
		return
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
	result, err := tx.Exec(`INSERT OR IGNORE INTO group_members(group_id,device_id,role,joined_at) VALUES(?,?,?,?)`, gid, in.DeviceID, "member", now)
	if err != nil {
		writeError(w, 500, "failed to add member")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeError(w, 500, "failed to determine membership change")
		return
	}
	if affected == 1 {
		if _, err = tx.Exec(`UPDATE groups SET key_version=key_version+1,rekey_required=1 WHERE id=?`, gid); err != nil {
			writeError(w, 500, "failed to advance group key epoch")
			return
		}
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
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "failed to add member")
		return
	}
	status := http.StatusOK
	if affected == 1 {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"ok": true, "rekey_required": affected == 1})
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
	if !a.isGroupOwner(gid, actor) {
		writeError(w, 403, "only the group owner can remove members")
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
	if _, err = tx.Exec(`UPDATE groups SET key_version=key_version+1,rekey_required=1 WHERE id=?`, gid); err != nil {
		writeError(w, 500, "failed to advance group key epoch")
		return
	}
	if _, err = tx.Exec(`DELETE FROM group_key_envelopes WHERE group_id=? AND recipient_device_id=?`, gid, id); err != nil {
		writeError(w, 500, "failed to revoke stored group-key envelopes")
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
	if err := decodeJSON(r, &in, maxEncryptedGroupMessageBody+16<<10); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	in.ID = strings.TrimSpace(in.ID)
	in.GroupID = strings.TrimSpace(in.GroupID)
	in.CreatedAt = strings.TrimSpace(in.CreatedAt)
	createdAt, ok := normalizeGroupMessageCreatedAt(in.CreatedAt)
	if in.GroupID == "" || !ok || !strings.HasPrefix(in.Body, "gme:v1:") || len(in.Body) > maxEncryptedGroupMessageBody || len(in.AttachmentIDs) > maxAttachmentsPerMessage {
		writeError(w, 400, "encrypted group message, group_id and created_at are required; maximum encrypted body is 64KB")
		return
	}
	if !a.isGroupMember(in.GroupID, sender) {
		writeError(w, 403, "device is not a group member")
		return
	}
	if !a.allowMessage(sender) {
		writeError(w, 429, "message rate limit exceeded")
		return
	}
	m, existing, err := a.insertGroupMessage(sender, in.GroupID, in.Body, createdAt, in.ID, in.AttachmentIDs)
	if err != nil {
		errorText := err.Error()
		if strings.Contains(errorText, "invalid group message key version") {
			writeError(w, http.StatusBadRequest, errorText)
			return
		}
		if strings.Contains(errorText, "device is not a group member") {
			writeError(w, http.StatusForbidden, errorText)
			return
		}
		if strings.Contains(errorText, "different message") || strings.Contains(errorText, "attachment") || strings.Contains(errorText, "too many") || strings.Contains(errorText, "group key rotation") {
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

func groupMessageKeyVersion(body string) (int, bool) {
	const prefix = "gme:v1:"
	if !strings.HasPrefix(body, prefix) {
		return 0, false
	}
	var envelope struct {
		Version    int    `json:"version"`
		Type       string `json:"type"`
		KeyVersion int    `json:"key_version"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(body[len(prefix):])), &envelope); err != nil {
		return 0, false
	}
	if envelope.Version != 1 || envelope.Type != "group_message" || envelope.KeyVersion < 1 || envelope.KeyVersion > maxGroupKeyVersion {
		return 0, false
	}
	return envelope.KeyVersion, true
}

func normalizeGroupMessageCreatedAt(value string) (string, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	return parsed.UTC().Format(time.RFC3339Nano), true
}

func (a *API) insertGroupMessage(sender, gid, body, createdAt, requested string, attachmentIDs []string) (GroupMessage, bool, error) {
	if !strings.HasPrefix(body, "gme:v1:") || len(body) > maxEncryptedGroupMessageBody {
		return GroupMessage{}, false, errors.New("group message body must be an encrypted gme:v1 envelope")
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
	version, ok := groupMessageKeyVersion(body)
	if !ok {
		return m, false, errors.New("invalid group message key version")
	}
	var currentVersion, rekeyRequired int
	if err = tx.QueryRow(`SELECT key_version,rekey_required FROM groups WHERE id=? AND EXISTS (SELECT 1 FROM group_members WHERE group_id=groups.id AND device_id=?)`, gid, sender).Scan(&currentVersion, &rekeyRequired); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return m, false, errors.New("device is not a group member")
		}
		return m, false, err
	}
	if rekeyRequired != 0 || version != currentVersion {
		return m, false, errors.New("group key rotation is required before sending")
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
