package httpapi

import (
	"database/sql"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
)

type SyncResponse struct {
	Messages        []Message      `json:"messages"`
	GroupMessages   []GroupMessage `json:"group_messages"`
	Groups          []Group        `json:"groups,omitempty"`
	GroupMembers    []GroupMember  `json:"group_members,omitempty"`
	RemovedGroupIDs []string       `json:"removed_group_ids,omitempty"`
	NextCursor      string         `json:"next_cursor,omitempty"`
	HasMore         bool           `json:"has_more"`
}

type syncEvent struct {
	Seq  int64
	Type string
	ID   string
}

func (a *API) Sync(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			writeError(w, 400, "limit must be between 1 and 500")
			return
		}
		limit = n
	}
	var cursor int64
	if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
		b, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			writeError(w, 400, "invalid cursor")
			return
		}
		cursor, err = strconv.ParseInt(string(b), 10, 64)
		if err != nil || cursor < 0 {
			writeError(w, 400, "invalid cursor")
			return
		}
	}
	rows, err := a.store.DB().Query(`SELECT seq,event_type,event_id FROM sync_events WHERE device_id=? AND seq>? ORDER BY seq ASC LIMIT ?`, deviceID, cursor, limit+1)
	if err != nil {
		writeError(w, 500, "failed to query sync")
		return
	}
	events := []syncEvent{}
	for rows.Next() {
		var e syncEvent
		if err := rows.Scan(&e.Seq, &e.Type, &e.ID); err != nil {
			writeError(w, 500, "failed to read sync row")
			return
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read sync rows")
		return
	}
	if err := rows.Close(); err != nil {
		writeError(w, 500, "failed to close sync rows")
		return
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	resp := SyncResponse{Messages: []Message{}, GroupMessages: []GroupMessage{}, Groups: []Group{}, GroupMembers: []GroupMember{}, RemovedGroupIDs: []string{}, HasMore: hasMore}
	for _, e := range events {
		switch e.Type {
		case "group_upsert":
			if !a.isGroupMember(e.ID, deviceID) {
				continue
			}
			var g Group
			err := a.store.DB().QueryRow(`SELECT id,name,owner_id,created_at FROM groups WHERE id=?`, e.ID).Scan(&g.ID, &g.Name, &g.OwnerID, &g.CreatedAt)
			if err == nil {
				resp.Groups = append(resp.Groups, g)
				members, merr := a.groupMembersForSync(e.ID)
				if merr != nil {
					writeError(w, 500, "failed to read group members")
					return
				}
				resp.GroupMembers = append(resp.GroupMembers, members...)
			} else if err != sql.ErrNoRows {
				writeError(w, 500, "failed to read group")
				return
			}
		case "group_removed":
			resp.RemovedGroupIDs = append(resp.RemovedGroupIDs, e.ID)
		case "message", "message_status":
			var m Message
			err := a.store.DB().QueryRow(`SELECT id,sender_id,recipient_id,body,created_at,status,COALESCE(delivered_at,''),COALESCE(server_seq,0) FROM messages WHERE id=?`, e.ID).Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &m.CreatedAt, &m.Status, &m.DeliveredAt, &m.ServerSeq)
			if err == nil {
				attachments, aerr := a.attachmentsForMessage(m.ID)
				if aerr != nil {
					writeError(w, 500, "failed to read message attachments")
					return
				}
				m.Attachments = attachments
				resp.Messages = append(resp.Messages, m)
			} else if err != sql.ErrNoRows {
				writeError(w, 500, "failed to read message")
				return
			}
		case "group_message":
			var groupID string
			if err := a.store.DB().QueryRow(`SELECT group_id FROM group_messages WHERE id=?`, e.ID).Scan(&groupID); err == nil {
				if !a.isGroupMember(groupID, deviceID) {
					continue
				}
			} else if err != sql.ErrNoRows {
				writeError(w, 500, "failed to read group membership")
				return
			}
			var m GroupMessage
			err := a.store.DB().QueryRow(`SELECT id,group_id,sender_id,body,created_at,server_seq FROM group_messages WHERE id=?`, e.ID).Scan(&m.ID, &m.GroupID, &m.SenderID, &m.Body, &m.CreatedAt, &m.ServerSeq)
			if err == nil {
				attachments, aerr := a.attachmentsForGroupMessage(m.ID)
				if aerr != nil {
					writeError(w, 500, "failed to read group message attachments")
					return
				}
				m.Attachments = attachments
				resp.GroupMessages = append(resp.GroupMessages, m)
			} else if err != sql.ErrNoRows {
				writeError(w, 500, "failed to read group message")
				return
			}
		}
	}
	if len(events) > 0 {
		resp.NextCursor = makeCursor(events[len(events)-1].Seq)
	} else if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
		resp.NextCursor = raw
	}
	writeJSON(w, 200, resp)
}
func (a *API) groupMembersForSync(groupID string) ([]GroupMember, error) {
	rows, err := a.store.DB().Query(`SELECT gm.group_id,gm.device_id,gm.role,gm.joined_at,d.name FROM group_members gm JOIN devices d ON d.id=gm.device_id WHERE gm.group_id=? ORDER BY gm.role DESC,d.name COLLATE NOCASE`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupMember
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.GroupID, &m.DeviceID, &m.Role, &m.JoinedAt, &m.Name); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func makeCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(seq, 10)))
}
