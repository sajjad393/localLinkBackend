package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type storedMessage struct {
	ID          string
	SenderID    string
	RecipientID string
	Body        string
	CreatedAt   string
	Status      string
	DeliveredAt string
	ServerSeq   int64
	Attachments []Attachment
}

var errMessageRecipientOnly = errors.New("device is not message recipient")

func (a *API) insertMessage(senderID, recipientID, body, requestedID string, attachmentIDs []string) (storedMessage, bool, error) {
	id := strings.TrimSpace(requestedID)
	if id == "" {
		id = uuid.NewString()
	}

	tx, err := a.store.DB().Begin()
	if err != nil {
		return storedMessage{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var existing storedMessage
	err = tx.QueryRow(`
SELECT id,sender_id,recipient_id,body,created_at,status,COALESCE(delivered_at,''),COALESCE(server_seq,0)
FROM messages WHERE id=?`, id).Scan(&existing.ID, &existing.SenderID, &existing.RecipientID, &existing.Body, &existing.CreatedAt, &existing.Status, &existing.DeliveredAt, &existing.ServerSeq)
	if err == nil {
		if existing.SenderID != senderID || existing.RecipientID != recipientID || existing.Body != body {
			return existing, true, errors.New("message id already belongs to different message")
		}
		if len(attachmentIDs) > 0 {
			existingIDs, err := a.attachmentIDsTx(tx, id, false)
			if err != nil {
				return existing, true, err
			}
			if len(existingIDs) == 0 {
				if err := a.validateAndAttachFiles(tx, id, false, senderID, attachmentIDs); err != nil {
					return existing, true, err
				}
			} else if !sameStringSlice(existingIDs, normalizeIDs(attachmentIDs)) {
				return existing, true, errors.New("message id already belongs to a different attachment set")
			}
		}
		if err := tx.Commit(); err != nil {
			return storedMessage{}, false, err
		}
		existing.Attachments, err = a.attachmentsForMessage(id)
		return existing, true, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storedMessage{}, false, err
	}

	if len(attachmentIDs) > maxAttachmentsPerMessage {
		return storedMessage{}, false, errors.New("too many attachments")
	}

	var seq int64
	if err := tx.QueryRow("SELECT COALESCE(MAX(server_seq),0)+1 FROM messages").Scan(&seq); err != nil {
		return storedMessage{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.Exec(`
INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status,server_seq)
VALUES(?,?,?,?,?,'sent',?)`, id, senderID, recipientID, body, now, seq)
	if err != nil {
		return storedMessage{}, false, err
	}
	if err := a.validateAndAttachFiles(tx, id, false, senderID, attachmentIDs); err != nil {
		return storedMessage{}, false, err
	}

	existing = storedMessage{ID: id, SenderID: senderID, RecipientID: recipientID, Body: body, CreatedAt: now, Status: "sent", ServerSeq: seq}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, senderID, "message", id); err != nil {
		return storedMessage{}, false, err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, recipientID, "message", id); err != nil {
		return storedMessage{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return storedMessage{}, false, err
	}
	existing.Attachments, err = a.attachmentsForMessage(id)
	if err != nil {
		return storedMessage{}, false, err
	}
	return existing, false, nil
}

func (a *API) markDelivered(id, deviceID string) (storedMessage, error) {
	tx, err := a.store.DB().Begin()
	if err != nil {
		return storedMessage{}, err
	}
	defer tx.Rollback()

	var m storedMessage
	err = tx.QueryRow(`
SELECT id,sender_id,recipient_id,body,created_at,status,COALESCE(delivered_at,''),COALESCE(server_seq,0)
FROM messages WHERE id=?`, id).Scan(&m.ID, &m.SenderID, &m.RecipientID, &m.Body, &m.CreatedAt, &m.Status, &m.DeliveredAt, &m.ServerSeq)
	if err != nil {
		return m, err
	}
	if m.RecipientID != deviceID {
		return m, errMessageRecipientOnly
	}
	if m.Status != "delivered" {
		deliveredAt := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err = tx.Exec("UPDATE messages SET status='delivered', delivered_at=? WHERE id=?", deliveredAt, id); err != nil {
			return m, err
		}
		if _, err = tx.Exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, m.SenderID, "message_status", m.ID); err != nil {
			return m, err
		}
		m.Status = "delivered"
		m.DeliveredAt = deliveredAt
	}
	if err := tx.Commit(); err != nil {
		return storedMessage{}, err
	}
	m.Attachments, err = a.attachmentsForMessage(id)
	return m, err
}

func (a *API) MessageAck(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	m, err := a.markDelivered(id, deviceID)
	if err != nil {
		if errors.Is(err, errMessageRecipientOnly) {
			writeError(w, http.StatusForbidden, "device is not message recipient")
		} else {
			writeError(w, http.StatusNotFound, "message not found")
		}
		return
	}
	writeJSON(w, http.StatusOK, Message{ID: m.ID, SenderID: m.SenderID, RecipientID: m.RecipientID, Body: m.Body, CreatedAt: m.CreatedAt, Status: m.Status, DeliveredAt: m.DeliveredAt, ServerSeq: m.ServerSeq, Attachments: m.Attachments})
}

func (a *API) attachmentIDsTx(tx *sql.Tx, messageID string, group bool) ([]string, error) {
	query := `SELECT file_id FROM message_attachments WHERE message_id=? ORDER BY position ASC,file_id ASC`
	if group {
		query = `SELECT file_id FROM group_message_attachments WHERE group_message_id=? ORDER BY position ASC,file_id ASC`
	}
	rows, err := tx.Query(query, messageID)
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

func normalizeIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, strings.TrimSpace(id))
	}
	return out
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
