package httpapi

// Administrator conversation review.
//
// Reading what users said to each other is the most sensitive thing an
// administrator can do from this console, so this file holds three rules:
//
//  1. The permission is "read_conversations", which only the owner role holds.
//     adminPermission grants the owner every permission, and no other role
//     lists this one, so operator, security and auditor admins receive 403.
//  2. Listing conversations returns metadata only — who, how many, when. Text
//     is returned solely by the two thread endpoints below.
//  3. Every thread read writes an admin_audit row before the text is returned,
//     so a review cannot happen without a trace. The listing, which exposes no
//     content, is not audited.
//
// Message bodies are stored as the sender wrote them: the server performs no
// encryption or decryption on them. That is why this endpoint can return text
// at all, and why the console's privacy page states the exposure plainly
// rather than claiming bodies are unreachable.

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const conversationPermission = "read_conversations"

type conversationParticipant struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

type conversationSummary struct {
	ID            string                    `json:"id"`
	Kind          string                    `json:"kind"`
	Participants  []conversationParticipant `json:"participants"`
	GroupID       string                    `json:"group_id,omitempty"`
	GroupName     string                    `json:"group_name,omitempty"`
	MemberCount   int                       `json:"member_count,omitempty"`
	MessageCount  int                       `json:"message_count"`
	LastMessageAt string                    `json:"last_message_at"`
}

type conversationMessage struct {
	ID             string                   `json:"id"`
	SenderID       string                   `json:"sender_id"`
	SenderName     string                   `json:"sender_name"`
	SenderUsername string                   `json:"sender_username"`
	RecipientID    string                   `json:"recipient_id,omitempty"`
	Body           string                   `json:"body"`
	CreatedAt      string                   `json:"created_at"`
	Status         string                   `json:"status,omitempty"`
	DeliveredAt    string                   `json:"delivered_at,omitempty"`
	ServerSeq      int64                    `json:"server_seq,omitempty"`
	Attachments    []conversationAttachment `json:"attachments,omitempty"`
}

// conversationAttachment describes an attachment to the conversation console.
// Every content URL it exposes goes through the owner-only, audit-logged
// admin media endpoint — never the user-facing file routes, whose bearer
// token the console does not hold.
type conversationAttachment struct {
	ID           string `json:"id"`
	OriginalName string `json:"original_name"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	IsImage      bool   `json:"is_image"`
	IsAudio      bool   `json:"is_audio"`
	DownloadURL  string `json:"download_url"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

const adminConversationMediaPath = "/api/v1/admin/conversation-media?file_id="

// adminConversationAttachments resolves ready attachments for one page of
// messages. table/column name the join table for direct ('message_attachments',
// 'message_id') or group ('group_message_attachments','group_message_id')
// messages; both are internal constants, never request input.
func (a *API) adminConversationAttachments(table, refColumn string, messageIDs []string) (map[string][]conversationAttachment, error) {
	result := make(map[string][]conversationAttachment, len(messageIDs))
	ids := uniqueNonEmptyIDs(messageIDs)
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := a.store.DB().Query(`SELECT ma.`+refColumn+`,f.id,f.original_name,f.content_type,f.size,COALESCE(f.width,0),COALESCE(f.height,0),f.is_image,COALESCE(f.thumbnail_path,'') FROM `+table+` ma JOIN files f ON f.id=ma.file_id WHERE ma.`+refColumn+` IN (`+placeholders+`) AND f.status='ready' ORDER BY ma.`+refColumn+`,ma.position,ma.file_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var messageID string
		var item conversationAttachment
		var imageFlag int
		var thumbnailPath string
		if err := rows.Scan(&messageID, &item.ID, &item.OriginalName, &item.ContentType, &item.Size, &item.Width, &item.Height, &imageFlag, &thumbnailPath); err != nil {
			return nil, err
		}
		item.IsImage = imageFlag != 0
		item.IsAudio = strings.HasPrefix(strings.ToLower(item.ContentType), "audio/")
		item.DownloadURL = adminConversationMediaPath + item.ID
		if thumbnailPath != "" {
			item.ThumbnailURL = adminConversationMediaPath + item.ID + "&thumb=1"
		}
		result[messageID] = append(result[messageID], item)
	}
	return result, rows.Err()
}

// fillConversationAttachments attaches media metadata to a page of direct or
// group messages before the JSON leaves the server.
func (a *API) fillConversationAttachments(groupThread bool, messages []conversationMessage) error {
	ids := make([]string, 0, len(messages))
	for _, m := range messages {
		ids = append(ids, m.ID)
	}
	table, key := "message_attachments", "message_id"
	if groupThread {
		table, key = "group_message_attachments", "group_message_id"
	}
	byMessage, err := a.adminConversationAttachments(table, key, ids)
	if err != nil {
		return err
	}
	for i := range messages {
		messages[i].Attachments = byMessage[messages[i].ID]
	}
	return nil
}

// deviceLabels resolves device ids to a display name and the account they
// belong to, in one query, so the console can show people rather than hashes.
func (a *API) deviceLabels(ids []string) (map[string]conversationParticipant, error) {
	out := make(map[string]conversationParticipant, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := a.store.DB().Query(
		`SELECT d.id, COALESCE(u.username,''), COALESCE(d.name,'')
		 FROM devices d LEFT JOIN users u ON u.id = d.user_id
		 WHERE d.id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p conversationParticipant
		if err := rows.Scan(&p.ID, &p.Username, &p.Name); err != nil {
			return nil, err
		}
		if p.Name == "" {
			p.Name = p.ID
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

// AdminConversations lists every conversation on the server, newest activity
// first. It deliberately carries no message text: an administrator reading
// snippets here would bypass the audit trail that the thread reads create.
func (a *API) AdminConversations(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, conversationPermission); !ok {
		return
	}
	params, err := parsePageParams(r, "admin-conversations")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	where := ""
	args := []any{}
	if params.Cursor != nil {
		where = `WHERE (last_at < ? OR (last_at = ? AND cid < ?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`
SELECT kind,cid,user_a,user_b,group_id,group_name,message_count,last_at FROM (
  SELECT 'direct' AS kind,
         CASE WHEN sender_id < recipient_id THEN sender_id ELSE recipient_id END || '|' ||
         CASE WHEN sender_id < recipient_id THEN recipient_id ELSE sender_id END AS cid,
         CASE WHEN sender_id < recipient_id THEN sender_id ELSE recipient_id END AS user_a,
         CASE WHEN sender_id < recipient_id THEN recipient_id ELSE sender_id END AS user_b,
         '' AS group_id, '' AS group_name,
         COUNT(*) AS message_count,
         MAX(created_at) AS last_at
  FROM messages GROUP BY cid
  UNION ALL
  SELECT 'group', g.id, '', '', g.id, COALESCE(g.name,''), COUNT(*), MAX(gm.created_at)
  FROM group_messages gm JOIN groups g ON g.id = gm.group_id GROUP BY g.id
)`+where+` ORDER BY last_at DESC, cid DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to list conversations")
		return
	}
	defer rows.Close()

	type rawConversation struct {
		kind, cid, userA, userB, groupID, groupName string
		count                                       int
		lastAt                                      string
	}
	raw := make([]rawConversation, 0, params.Limit+1)
	deviceIDs := make([]string, 0, 8)
	groupIDs := make([]string, 0, 4)
	seenDevice := map[string]bool{}
	seenGroup := map[string]bool{}
	for rows.Next() {
		var c rawConversation
		if err := rows.Scan(&c.kind, &c.cid, &c.userA, &c.userB, &c.groupID, &c.groupName, &c.count, &c.lastAt); err != nil {
			writeError(w, 500, "failed to read conversation")
			return
		}
		raw = append(raw, c)
		for _, id := range []string{c.userA, c.userB} {
			if id != "" && !seenDevice[id] {
				seenDevice[id] = true
				deviceIDs = append(deviceIDs, id)
			}
		}
		if c.groupID != "" && !seenGroup[c.groupID] {
			seenGroup[c.groupID] = true
			groupIDs = append(groupIDs, c.groupID)
		}
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read conversations")
		return
	}
	rows.Close()

	labels, err := a.deviceLabels(deviceIDs)
	if err != nil {
		writeError(w, 500, "failed to resolve participants")
		return
	}
	memberCounts := map[string]int{}
	if len(groupIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(groupIDs)), ",")
		args := make([]any, 0, len(groupIDs))
		for _, id := range groupIDs {
			args = append(args, id)
		}
		countRows, err := a.store.DB().Query(`SELECT group_id, COUNT(*) FROM group_members WHERE group_id IN (`+placeholders+`) GROUP BY group_id`, args...)
		if err == nil {
			for countRows.Next() {
				var groupID string
				var n int
				if err := countRows.Scan(&groupID, &n); err == nil {
					memberCounts[groupID] = n
				}
			}
			countRows.Close()
		}
	}

	out := make([]conversationSummary, 0, len(raw))
	for _, c := range raw {
		summary := conversationSummary{
			ID:            c.cid,
			Kind:          c.kind,
			MessageCount:  c.count,
			LastMessageAt: c.lastAt,
		}
		if c.kind == "group" {
			summary.GroupID = c.groupID
			summary.GroupName = c.groupName
			summary.MemberCount = memberCounts[c.groupID]
		} else {
			for _, id := range []string{c.userA, c.userB} {
				label, ok := labels[id]
				if !ok {
					label = conversationParticipant{ID: id, Name: id}
				}
				summary.Participants = append(summary.Participants, label)
			}
		}
		out = append(out, summary)
	}

	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Conversations   []conversationSummary `json:"conversations"`
		ContentIncluded bool                  `json:"content_included"`
		Access          string                `json:"access"`
		PageInfo
	}{Conversations: out, ContentIncluded: false, Access: "owner"}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-conversations", last.LastMessageAt, last.ID, true)
	}
	writeJSON(w, 200, resp)
}

// AdminConversation returns the messages exchanged between two devices, newest
// first, and records the review in the audit log.
func (a *API) AdminConversation(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, conversationPermission)
	if !ok {
		return
	}
	userA := strings.TrimSpace(r.URL.Query().Get("user_a"))
	userB := strings.TrimSpace(r.URL.Query().Get("user_b"))
	if userA == "" || userB == "" || userA == userB || len(userA) > 128 || len(userB) > 128 {
		writeError(w, 400, "user_a and user_b must name two different devices")
		return
	}
	if userA > userB {
		userA, userB = userB, userA
	}
	params, err := parsePageParams(r, "admin-conversation:"+userA+"|"+userB)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	where := ""
	args := []any{userA, userB, userB, userA}
	if params.Cursor != nil {
		where = ` AND (m.created_at < ? OR (m.created_at = ? AND m.id < ?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`
SELECT m.id,m.sender_id,COALESCE(sd.name,''),COALESCE(su.username,''),m.recipient_id,m.body,m.created_at,m.status,COALESCE(m.delivered_at,''),COALESCE(m.server_seq,0)
FROM messages m
LEFT JOIN devices sd ON sd.id = m.sender_id
LEFT JOIN users su ON su.id = sd.user_id
WHERE ((m.sender_id=? AND m.recipient_id=?) OR (m.sender_id=? AND m.recipient_id=?))`+where+`
ORDER BY m.created_at DESC, m.id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to read conversation")
		return
	}
	defer rows.Close()
	out := make([]conversationMessage, 0, params.Limit+1)
	for rows.Next() {
		var m conversationMessage
		if err := rows.Scan(&m.ID, &m.SenderID, &m.SenderName, &m.SenderUsername, &m.RecipientID, &m.Body, &m.CreatedAt, &m.Status, &m.DeliveredAt, &m.ServerSeq); err != nil {
			writeError(w, 500, "failed to read message")
			return
		}
		if m.SenderName == "" {
			m.SenderName = m.SenderID
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read conversation")
		return
	}

	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	if err := a.fillConversationAttachments(false, out); err != nil {
		writeError(w, 500, "failed to read attachments")
		return
	}

	// Recorded before the text leaves the server, whether or not any message
	// matched: the operator opened a conversation, and that is the fact worth
	// keeping.
	a.adminAudit(admin.ID, "admin_conversation_viewed", userA+"|"+userB, fmt.Sprintf("messages=%d", len(out)))

	labels, err := a.deviceLabels([]string{userA, userB})
	if err != nil {
		writeError(w, 500, "failed to resolve participants")
		return
	}
	participants := make([]conversationParticipant, 0, 2)
	for _, id := range []string{userA, userB} {
		label, ok := labels[id]
		if !ok {
			label = conversationParticipant{ID: id, Name: id}
		}
		participants = append(participants, label)
	}

	resp := struct {
		Kind            string                    `json:"kind"`
		Participants    []conversationParticipant `json:"participants"`
		Messages        []conversationMessage     `json:"messages"`
		ContentIncluded bool                      `json:"content_included"`
		Audited         bool                      `json:"audited"`
		Access          string                    `json:"access"`
		PageInfo
	}{Kind: "direct", Participants: participants, Messages: out, ContentIncluded: true, Audited: true, Access: "owner"}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-conversation:"+userA+"|"+userB, last.CreatedAt, last.ID, true)
	}
	writeJSON(w, 200, resp)
}

// AdminGroupConversation returns the messages posted in one group and records
// the review in the audit log.
func (a *API) AdminGroupConversation(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, conversationPermission)
	if !ok {
		return
	}
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupID == "" || len(groupID) > 128 {
		writeError(w, 400, "group_id is required")
		return
	}
	var groupName string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(name,'') FROM groups WHERE id=?`, groupID).Scan(&groupName); err != nil {
		writeError(w, 404, "group not found")
		return
	}
	params, err := parsePageParams(r, "admin-group-conversation:"+groupID)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	where := ""
	args := []any{groupID}
	if params.Cursor != nil {
		where = ` AND (gm.created_at < ? OR (gm.created_at = ? AND gm.id < ?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`
SELECT gm.id,gm.sender_id,COALESCE(sd.name,''),COALESCE(su.username,''),gm.body,gm.created_at,gm.server_seq
FROM group_messages gm
LEFT JOIN devices sd ON sd.id = gm.sender_id
LEFT JOIN users su ON su.id = sd.user_id
WHERE gm.group_id=?`+where+`
ORDER BY gm.created_at DESC, gm.id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, 500, "failed to read group conversation")
		return
	}
	defer rows.Close()
	out := make([]conversationMessage, 0, params.Limit+1)
	for rows.Next() {
		var m conversationMessage
		if err := rows.Scan(&m.ID, &m.SenderID, &m.SenderName, &m.SenderUsername, &m.Body, &m.CreatedAt, &m.ServerSeq); err != nil {
			writeError(w, 500, "failed to read group message")
			return
		}
		if m.SenderName == "" {
			m.SenderName = m.SenderID
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "failed to read group conversation")
		return
	}

	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	if err := a.fillConversationAttachments(true, out); err != nil {
		writeError(w, 500, "failed to read attachments")
		return
	}

	a.adminAudit(admin.ID, "admin_group_conversation_viewed", "group:"+groupID, fmt.Sprintf("messages=%d", len(out)))

	var memberCount int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM group_members WHERE group_id=?`, groupID).Scan(&memberCount)

	resp := struct {
		Kind            string                `json:"kind"`
		GroupID         string                `json:"group_id"`
		GroupName       string                `json:"group_name"`
		MemberCount     int                   `json:"member_count"`
		Messages        []conversationMessage `json:"messages"`
		ContentIncluded bool                  `json:"content_included"`
		Audited         bool                  `json:"audited"`
		Access          string                `json:"access"`
		PageInfo
	}{Kind: "group", GroupID: groupID, GroupName: groupName, MemberCount: memberCount, Messages: out, ContentIncluded: true, Audited: true, Access: "owner"}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("admin-group-conversation:"+groupID, last.CreatedAt, last.ID, true)
	}
	writeJSON(w, 200, resp)
}

// AdminConversationMedia streams one attachment's bytes for the owner who is
// reviewing a conversation. The console fetches it with the admin bearer
// header and renders the returned blob as an object URL, so this endpoint
// deliberately stays header-only and never invites cookie or query-token
// auth. Every fetch is recorded in the audit log, the same rule that governs
// thread reads. '?thumb=1' serves the stored image thumbnail instead.
func (a *API) AdminConversationMedia(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, conversationPermission)
	if !ok {
		return
	}
	fileID := strings.TrimSpace(r.URL.Query().Get("file_id"))
	if !validClientFileID(fileID) {
		writeError(w, 400, "file_id is invalid")
		return
	}
	wantThumb := r.URL.Query().Get("thumb") == "1"
	var originalName, contentType, storagePath, thumbnailPath, status string
	err := a.store.DB().QueryRow(
		`SELECT COALESCE(original_name,''),COALESCE(content_type,''),storage_path,COALESCE(thumbnail_path,''),status FROM files WHERE id=?`,
		fileID).Scan(&originalName, &contentType, &storagePath, &thumbnailPath, &status)
	if err != nil || status != "ready" || storagePath == "" {
		writeError(w, 404, "attachment not found")
		return
	}
	servePath := storagePath
	if wantThumb {
		if thumbnailPath == "" {
			writeError(w, 404, "thumbnail not found")
			return
		}
		servePath = thumbnailPath
		// createThumbnail writes '<id>-thumb.png' files.
		contentType = "image/png"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	a.adminAudit(admin.ID, "admin_conversation_media_viewed", fileID, fmt.Sprintf("thumb=%v name=%s", wantThumb, originalName))

	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	lower := strings.ToLower(contentType)
	if wantThumb || strings.HasPrefix(lower, "audio/") || strings.HasPrefix(lower, "image/") || strings.HasPrefix(lower, "video/") {
		header.Set("Content-Disposition", contentDispositionFilename("inline", originalName))
	} else {
		header.Set("Content-Disposition", contentDispositionFilename("attachment", originalName))
	}
	http.ServeFile(w, r, servePath)
}

// contentDispositionFilename builds an RFC 5987-safe attachment disposition so
// arbitrary uploader-chosen filenames cannot smuggle header characters.
func contentDispositionFilename(kind, name string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == '"' || r == '\\' || r == ';':
			return '_'
		case r > 126:
			return -1
		default:
			return r
		}
	}, name)
	if clean == "" {
		clean = "download"
	}
	if !strings.EqualFold(clean, name) || strings.ContainsAny(clean, " ;") {
		// Always pair the ASCII fallback with the exact UTF-8 form; quoting the
		// UTF-8 form keeps headers parseable while preserving every character.
		return kind + "; filename=\"" + clean + "\"; filename*=UTF-8''" + url.PathEscape(name)
	}
	return kind + "; filename=\"" + clean + "\""
}
