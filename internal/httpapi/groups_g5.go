package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Bundle G5: per-member receipts + typing for groups on the server road.
//
//   - WS "group_receipt" {group_id, status: delivered|read, message_ids[]}:
//     kept per (message, member) and sent to the writer when online. The
//     writer pulls what it missed with GET /api/v1/groups/receipts.
//   - WS "group_typing" {group_id, enabled}: passed to the members online
//     now (not kept), at most one per 2 s per phone and group.

const maxGroupReceiptIDs = 200

type GroupReceipt struct {
	MessageID   string `json:"message_id"`
	GroupID     string `json:"group_id"`
	DeviceID    string `json:"device_id"`
	DeliveredAt string `json:"delivered_at,omitempty"`
	ReadAt      string `json:"read_at,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

var errGroupReceiptInput = errors.New("group_id, status delivered|read and 1-200 message_ids are required")

func (a *API) handleGroupReceipt(deviceID string, msg WSMessage) error {
	gid := strings.TrimSpace(msg.GroupID)
	status := strings.TrimSpace(msg.Status)
	ids := msg.MessageIDs
	if len(ids) == 0 && strings.TrimSpace(msg.ID) != "" {
		ids = []string{msg.ID}
	}
	if gid == "" || (status != "delivered" && status != "read") || len(ids) == 0 || len(ids) > maxGroupReceiptIDs {
		return errGroupReceiptInput
	}
	if !a.isGroupMember(gid, deviceID) {
		return errors.New("device is not a group member")
	}
	// Fixed width so text order = time order ("since").
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	for _, raw := range ids {
		mid := strings.TrimSpace(raw)
		if mid == "" || len(mid) > 128 || hasControlChars(mid) {
			continue
		}
		var sender string
		if err := a.store.DB().QueryRow(`SELECT sender_id FROM group_messages WHERE id=? AND group_id=?`, mid, gid).Scan(&sender); err != nil {
			continue
		}
		if sender == deviceID {
			continue
		}
		delivered, read := now, ""
		if status == "read" {
			read = now
		}
		// delivered stays the first time; read sets delivered too.
		if _, err := a.store.DB().Exec(`INSERT INTO group_receipts(message_id,group_id,device_id,sender_id,delivered_at,read_at,updated_at)
			VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(message_id,device_id) DO UPDATE SET
			  delivered_at=CASE WHEN group_receipts.delivered_at='' THEN excluded.delivered_at ELSE group_receipts.delivered_at END,
			  read_at=CASE WHEN group_receipts.read_at='' THEN excluded.read_at ELSE group_receipts.read_at END,
			  updated_at=excluded.updated_at`,
			mid, gid, deviceID, sender, delivered, read, now); err != nil {
			continue
		}
		a.hub.SendTo(sender, mustJSON(WSMessage{Type: "group_receipt", ID: mid, GroupID: gid, DeviceID: deviceID, Status: status, CreatedAt: now}))
	}
	return nil
}

var (
	groupTypingMu   sync.Mutex
	groupTypingLast = map[string]time.Time{}
)

func (a *API) handleGroupTyping(deviceID string, msg WSMessage) {
	gid := strings.TrimSpace(msg.GroupID)
	if gid == "" || len(gid) > 128 || !a.isGroupMember(gid, deviceID) {
		return
	}
	on := msg.Enabled == nil || *msg.Enabled
	key := deviceID + "|" + gid
	groupTypingMu.Lock()
	if on {
		if last, ok := groupTypingLast[key]; ok && time.Since(last) < 2*time.Second {
			groupTypingMu.Unlock()
			return
		}
		groupTypingLast[key] = time.Now()
	} else {
		delete(groupTypingLast, key)
	}
	if len(groupTypingLast) > 5000 {
		groupTypingLast = map[string]time.Time{}
	}
	groupTypingMu.Unlock()
	members, _ := a.groupMemberIDs(gid)
	enabled := on
	payload := mustJSON(WSMessage{Type: "group_typing", GroupID: gid, SenderID: deviceID, Enabled: &enabled})
	for _, id := range members {
		if id != deviceID {
			a.hub.SendTo(id, payload)
		}
	}
}

// GroupReceipts: GET /api/v1/groups/receipts?group_id=&since= - receipts for
// MY messages in that group changed after `since` (RFC3339, optional).
func (a *API) GroupReceipts(w http.ResponseWriter, r *http.Request) {
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
	since := strings.TrimSpace(r.URL.Query().Get("since"))
	rows, err := a.store.DB().Query(`SELECT message_id,group_id,device_id,delivered_at,read_at,updated_at FROM group_receipts
		WHERE sender_id=? AND group_id=? AND updated_at>? ORDER BY updated_at ASC LIMIT 2000`, deviceID, gid, since)
	if err != nil {
		writeError(w, 500, "failed to list receipts")
		return
	}
	defer rows.Close()
	out := make([]GroupReceipt, 0)
	for rows.Next() {
		var g GroupReceipt
		if err := rows.Scan(&g.MessageID, &g.GroupID, &g.DeviceID, &g.DeliveredAt, &g.ReadAt, &g.UpdatedAt); err != nil {
			writeError(w, 500, "failed to read receipts")
			return
		}
		out = append(out, g)
	}
	writeJSON(w, 200, map[string]any{"receipts": out})
}
