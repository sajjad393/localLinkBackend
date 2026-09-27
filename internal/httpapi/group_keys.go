package httpapi

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
)

const (
	maxGroupKeyEnvelopeSize = 16 << 10
	maxGroupKeyVersion      = 1_000_000
)

type GroupKeyEnvelope struct {
	GroupID     string `json:"group_id"`
	KeyVersion  int    `json:"key_version"`
	SenderID    string `json:"sender_id"`
	RecipientID string `json:"recipient_id"`
	Envelope    string `json:"envelope"`
	CreatedAt   string `json:"created_at"`
}

func (a *API) ListGroupKeyEnvelopes(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "group_id is required")
		return
	}
	if !a.isGroupMember(groupID, deviceID) {
		writeError(w, http.StatusForbidden, "device is not a group member")
		return
	}

	rows, err := a.store.DB().Query(`
SELECT group_id,key_version,sender_id,recipient_device_id,envelope,created_at
FROM group_key_envelopes
WHERE group_id=? AND recipient_device_id=?
ORDER BY key_version ASC`, groupID, deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read group key envelopes")
		return
	}
	defer rows.Close()
	out := make([]GroupKeyEnvelope, 0)
	for rows.Next() {
		var item GroupKeyEnvelope
		if err := rows.Scan(&item.GroupID, &item.KeyVersion, &item.SenderID, &item.RecipientID, &item.Envelope, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read group key envelope")
			return
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to iterate group key envelopes")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) DistributeGroupKeyEnvelopes(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in struct {
		GroupID    string `json:"group_id"`
		KeyVersion int    `json:"key_version"`
		Envelopes  []struct {
			RecipientID string `json:"recipient_id"`
			Envelope    string `json:"envelope"`
		} `json:"envelopes"`
	}
	if err := decodeJSON(r, &in, 256<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.GroupID = strings.TrimSpace(in.GroupID)
	if in.GroupID == "" || in.KeyVersion < 1 || in.KeyVersion > maxGroupKeyVersion {
		writeError(w, http.StatusBadRequest, "invalid group key metadata")
		return
	}
	if !a.isGroupOwner(in.GroupID, actor) {
		writeError(w, http.StatusForbidden, "only the group owner can distribute group keys")
		return
	}
	if len(in.Envelopes) > maxGroupMembers {
		writeError(w, http.StatusBadRequest, "too many group key envelopes")
		return
	}

	tx, err := a.store.DB().Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start group key transaction")
		return
	}
	defer tx.Rollback()

	var currentVersion, rekeyRequired int
	if err := tx.QueryRow(`SELECT key_version,rekey_required FROM groups WHERE id=?`, in.GroupID).Scan(&currentVersion, &rekeyRequired); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "group not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read group key state")
		}
		return
	}
	if in.KeyVersion != currentVersion {
		writeError(w, http.StatusConflict, "group key version does not match the current group epoch")
		return
	}

	members, err := a.groupMemberIDsTx(tx, in.GroupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read group members")
		return
	}
	requiredRecipients := make(map[string]bool, len(members))
	for _, id := range members {
		if id != actor {
			requiredRecipients[id] = true
		}
	}

	// A committed epoch can be retried safely when the exact same envelope set
	// is submitted after a lost response.
	rowsExisting, err := tx.Query(`SELECT recipient_device_id,envelope FROM group_key_envelopes WHERE group_id=? AND key_version=?`, in.GroupID, in.KeyVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read existing group key envelopes")
		return
	}
	existing := make(map[string]string)
	for rowsExisting.Next() {
		var recipient, envelope string
		if err := rowsExisting.Scan(&recipient, &envelope); err != nil {
			rowsExisting.Close()
			writeError(w, http.StatusInternalServerError, "failed to read existing group key envelope")
			return
		}
		existing[recipient] = envelope
	}
	if err := rowsExisting.Err(); err != nil {
		rowsExisting.Close()
		writeError(w, http.StatusInternalServerError, "failed to iterate existing group key envelopes")
		return
	}
	rowsExisting.Close()

	requested := make(map[string]string, len(in.Envelopes))
	for _, env := range in.Envelopes {
		recipient := strings.TrimSpace(env.RecipientID)
		envelope := strings.TrimSpace(env.Envelope)
		if recipient == "" || recipient == actor || !requiredRecipients[recipient] || requested[recipient] != "" || envelope == "" || len(envelope) > maxGroupKeyEnvelopeSize || !strings.HasPrefix(envelope, "gke:v1:") {
			writeError(w, http.StatusBadRequest, "invalid group key envelope")
			return
		}
		requested[recipient] = envelope
	}
	if len(requested) != len(requiredRecipients) {
		writeError(w, http.StatusBadRequest, "group key envelope set must cover every current member except the owner")
		return
	}
	if len(existing) > 0 || rekeyRequired == 0 {
		if len(existing) == len(requested) {
			exact := true
			for recipient, envelope := range existing {
				if requested[recipient] != envelope {
					exact = false
					break
				}
			}
			if exact {
				if rekeyRequired != 0 {
					if _, err := tx.Exec(`UPDATE groups SET rekey_required=0 WHERE id=? AND key_version=?`, in.GroupID, in.KeyVersion); err != nil {
						writeError(w, http.StatusInternalServerError, "failed to commit existing group key epoch")
						return
					}
				}
				if err := tx.Commit(); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to commit idempotent group key request")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "group_id": in.GroupID, "key_version": in.KeyVersion, "envelope_count": len(in.Envelopes), "idempotent": true})
				return
			}
		}
		writeError(w, http.StatusConflict, "group key version already exists with different envelopes")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for recipient, envelope := range requested {
		if _, err := tx.Exec(`
INSERT INTO group_key_envelopes(group_id,key_version,sender_id,recipient_device_id,envelope,created_at)
VALUES(?,?,?,?,?,?)`, in.GroupID, in.KeyVersion, actor, recipient, envelope, now); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to store group key envelope")
			return
		}
	}
	if _, err := tx.Exec(`UPDATE groups SET rekey_required=0 WHERE id=? AND key_version=?`, in.GroupID, in.KeyVersion); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit group key epoch")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit group key envelopes")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "group_id": in.GroupID, "key_version": in.KeyVersion, "envelope_count": len(in.Envelopes)})
}
