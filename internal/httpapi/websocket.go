package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"locallink/backend/internal/ws"
)

type WSMessage struct {
	Type                string       `json:"type"`
	ID                  string       `json:"id,omitempty"`
	SenderID            string       `json:"sender_id,omitempty"`
	RecipientID         string       `json:"recipient_id,omitempty"`
	Body                string       `json:"body,omitempty"`
	CreatedAt           string       `json:"created_at,omitempty"`
	Status              string       `json:"status,omitempty"`
	ClientMessageID     string       `json:"client_message_id,omitempty"`
	ServerSeq           int64        `json:"server_seq,omitempty"`
	GroupID             string       `json:"group_id,omitempty"`
	AttachmentIDs       []string     `json:"attachment_ids,omitempty"`
	Attachments         []Attachment `json:"attachments,omitempty"`
	CallID              string       `json:"call_id,omitempty"`
	Reason              string       `json:"reason,omitempty"`
	TransportMode       string       `json:"transport_mode,omitempty"`
	SelectedMediaCodec  string       `json:"selected_media_codec,omitempty"`
	Error               string       `json:"error,omitempty"`
	DeviceID            string       `json:"device_id,omitempty"`
	NetworkStatus       string       `json:"network_status,omitempty"`
	ServerConnected     bool         `json:"server_connected,omitempty"`
	WiFiDirectConnected bool         `json:"wifi_direct_connected,omitempty"`
	StatusUpdatedAt     string       `json:"status_updated_at,omitempty"`
	LastSeenAt          string       `json:"last_seen_at,omitempty"`
	SigningPublicKey    string       `json:"signing_public_key,omitempty"`
	WiFiControlCapable  bool         `json:"wifi_control_capable,omitempty"`
	ManagedDevice       bool         `json:"managed_device,omitempty"`
	WiFiEnabled         bool         `json:"wifi_enabled,omitempty"`
	LanAvailable        bool         `json:"lan_available,omitempty"`
	MeshAvailable       bool         `json:"mesh_available,omitempty"`
}

func (a *API) WebSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "GET is required")
		return
	}
	if !headerHasToken(r.Header.Values("Upgrade"), "websocket") || !headerHasToken(r.Header.Values("Connection"), "upgrade") || strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		writeError(w, http.StatusBadRequest, "invalid websocket upgrade headers")
		return
	}
	deviceID := strings.TrimSpace(r.Header.Get("X-Device-ID"))
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if deviceID == "" || auth == "" || !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		writeError(w, http.StatusUnauthorized, "device credentials must use X-Device-ID and Authorization: Bearer")
		return
	}
	if strings.TrimSpace(auth[len("Bearer "):]) == "" {
		writeError(w, http.StatusUnauthorized, "device credentials are required")
		return
	}
	if r.URL.Query().Get("token") != "" || r.URL.Query().Get("device_id") != "" {
		writeError(w, http.StatusForbidden, "websocket credentials must not be sent in query parameters")
		return
	}
	if _, ok := a.authorizeDevice(r); !ok {
		writeError(w, http.StatusUnauthorized, "invalid device credentials")
		return
	}

	setSecurityHeaders(w)
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if !validWebSocketKey(key) {
		writeError(w, http.StatusBadRequest, "websocket upgrade required")
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, http.StatusInternalServerError, "websocket unsupported")
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	if err := ws.Handshake(conn, key); err != nil {
		_ = conn.Close()
		return
	}

	client := ws.NewClient(deviceID, conn)
	a.hub.Add(deviceID, client)
	if d, changed := a.setPresence(deviceID, true, false, DevicePresence{}); changed {
		a.broadcastPresence(deviceID, d)
	}
	defer func() {
		a.hub.Remove(deviceID, client)
		client.Close()
		if !a.hub.HasDevice(deviceID) {
			d, _ := a.setPresence(deviceID, false, false, DevicePresence{})
			a.broadcastPresence(deviceID, d)
		}
	}()

	_ = client.WriteText(mustJSON(WSMessage{Type: "ready", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}))
	a.pushNetworkPolicyToDevice(deviceID)

	stopPing := make(chan struct{})
	defer close(stopPing)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := client.WritePing(nil); err != nil {
					_ = conn.Close()
					return
				}
			case <-stopPing:
				return
			}
		}
	}()

	for {
		_ = client.SetDeadline(time.Now().Add(90 * time.Second))
		opcode, payload, err := ws.ReadFrame(rw.Reader)
		if err != nil {
			return
		}
		switch opcode {
		case ws.TextFrame:
			var msg WSMessage
			if json.Unmarshal(payload, &msg) != nil {
				_ = client.WriteText(mustJSON(WSMessage{Type: "error", Error: "invalid JSON"}))
				continue
			}
			switch msg.Type {
			case "presence_update":
				if !a.allowPresenceUpdate(deviceID) {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", Error: "presence update rate limit exceeded"}))
					continue
				}
				presence := DevicePresence{ServerConnected: true, WiFiDirectConnected: msg.WiFiDirectConnected, LanAvailable: msg.LanAvailable, MeshAvailable: msg.MeshAvailable, SigningPublicKey: msg.SigningPublicKey, WiFiControlCapable: msg.WiFiControlCapable, ManagedDevice: msg.ManagedDevice, WiFiEnabled: msg.WiFiEnabled}
				d, changed := a.setPresence(deviceID, true, msg.WiFiDirectConnected, presence)
				if changed {
					a.broadcastPresence(deviceID, d)
				}
				continue
			case "call_invite":
				if strings.TrimSpace(msg.TransportMode) != "offline_mesh" {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "only offline_mesh call transport is supported"}))
					continue
				}
				if strings.TrimSpace(msg.RecipientID) == "" || strings.TrimSpace(msg.CallID) == "" {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "recipient_id and call_id are required for call_invite"}))
					continue
				}
				if msg.RecipientID == deviceID || !a.deviceExists(msg.RecipientID) {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "recipient device not found"}))
					continue
				}
				if !a.allowCallSignaling(deviceID) {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "call signaling rate limit exceeded"}))
					continue
				}
				call, existing, err := a.createCall(deviceID, msg.RecipientID, msg.CallID)
				if err != nil {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: err.Error()}))
					continue
				}
				_ = client.WriteText(mustJSON(WSMessage{Type: "call_invite_ack", CallID: call.ID, SenderID: call.CallerID, RecipientID: call.CalleeID, Status: call.Status, CreatedAt: call.StartedAt}))
				if !existing {
					a.hub.SendTo(call.CalleeID, mustJSON(WSMessage{Type: "call_invite", CallID: call.ID, SenderID: call.CallerID, RecipientID: call.CalleeID, Status: call.Status, CreatedAt: call.StartedAt}))
				}
			case "call_accept", "call_reject", "call_end", "call_busy":
				if strings.TrimSpace(msg.CallID) == "" {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", Error: "call_id is required"}))
					continue
				}
				call, err := a.callForParticipant(msg.CallID, deviceID)
				if err != nil {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: err.Error()}))
					continue
				}
				if !a.allowCallSignaling(deviceID) {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "call signaling rate limit exceeded"}))
					continue
				}
				status := "ended"
				if msg.Type == "call_accept" {
					if strings.TrimSpace(msg.TransportMode) != "offline_mesh" {
						_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "only offline_mesh call transport is supported"}))
						continue
					}
					if deviceID != call.CalleeID || call.Status != "ringing" {
						_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "only the callee can accept a ringing call"}))
						continue
					}
					status = "connected"
				} else if msg.Type == "call_reject" || msg.Type == "call_busy" {
					if deviceID != call.CalleeID || call.Status != "ringing" {
						_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "only the callee can reject a ringing call"}))
						continue
					}
					status = "rejected"
					if msg.Type == "call_busy" && msg.Reason == "" {
						msg.Reason = "busy"
					}
				} else if call.Status != "ringing" && call.Status != "connected" {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "call is no longer active"}))
					continue
				}
				if msg.Type == "call_end" && strings.TrimSpace(msg.Reason) == "missed" {
					status = "missed"
				} else if msg.Type == "call_end" && strings.TrimSpace(msg.Reason) == "timeout" {
					status = "canceled"
				} else if msg.Type == "call_end" && strings.TrimSpace(msg.Reason) == "failed" {
					status = "failed"
				}
				updated, err := a.updateCallState(call.ID, deviceID, status, msg.Reason)
				if err != nil {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: err.Error()}))
					continue
				}
				peerID := updated.CalleeID
				if peerID == deviceID {
					peerID = updated.CallerID
				}
				_ = client.WriteText(mustJSON(WSMessage{Type: "call_state", CallID: updated.ID, SenderID: updated.CallerID, RecipientID: updated.CalleeID, Status: updated.Status, CreatedAt: updated.StartedAt, Reason: updated.EndReason}))
				a.hub.SendTo(peerID, mustJSON(WSMessage{Type: "call_state", CallID: updated.ID, SenderID: updated.CallerID, RecipientID: updated.CalleeID, Status: updated.Status, CreatedAt: updated.StartedAt, Reason: updated.EndReason}))
			case "call_heartbeat":
				if strings.TrimSpace(msg.CallID) == "" {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "call_id is required"}))
					continue
				}
				if !a.allowCallHeartbeat(deviceID) {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: "call heartbeat rate limit exceeded"}))
					continue
				}
				call, err := a.TouchCall(msg.CallID, deviceID)
				if err != nil {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", CallID: msg.CallID, Error: err.Error()}))
					continue
				}
				_ = client.WriteText(mustJSON(WSMessage{Type: "call_heartbeat_ack", CallID: call.ID, Status: call.Status, CreatedAt: call.LastActivityAt}))
			case "ack_delivery":
				if strings.TrimSpace(msg.ID) == "" {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", Error: "id is required for ack_delivery"}))
					continue
				}
				m, err := a.markDelivered(msg.ID, deviceID)
				if err != nil {
					errorText := "message not found"
					if errors.Is(err, errMessageRecipientOnly) {
						errorText = "device is not message recipient"
					}
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: errorText}))
					continue
				}
				a.hub.SendTo(m.SenderID, mustJSON(WSMessage{Type: "delivery_ack", ID: m.ID, SenderID: m.SenderID, RecipientID: m.RecipientID, Status: m.Status, CreatedAt: m.DeliveredAt, ServerSeq: m.ServerSeq, Attachments: m.Attachments}))
			case "send_group_message":
				createdAt, timestampOK := normalizeGroupMessageCreatedAt(msg.CreatedAt)
				if strings.TrimSpace(msg.GroupID) == "" || !timestampOK || !strings.HasPrefix(msg.Body, "gme:v1:") || len(msg.Body) > maxEncryptedGroupMessageBody || len(msg.AttachmentIDs) > maxAttachmentsPerMessage {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: "encrypted group message, group_id and valid created_at are required; max 10 attachments"}))
					continue
				}
				if !a.isGroupMember(msg.GroupID, deviceID) {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: "device is not a group member"}))
					continue
				}
				if !a.allowMessage(deviceID) {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: "message rate limit exceeded"}))
					continue
				}
				m, existing, err := a.insertGroupMessage(deviceID, msg.GroupID, msg.Body, createdAt, msg.ID, msg.AttachmentIDs)
				if err != nil {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: err.Error()}))
					continue
				}
				_ = client.WriteText(mustJSON(WSMessage{Type: "group_message_ack", ID: m.ID, GroupID: m.GroupID, SenderID: m.SenderID, Body: m.Body, CreatedAt: m.CreatedAt, ServerSeq: m.ServerSeq, ClientMessageID: msg.ID, Attachments: m.Attachments}))
				if !existing {
					a.broadcastGroupMessage(m)
				}
			case "send_message":
				if strings.TrimSpace(msg.ID) == "" || len(msg.ID) > 128 || hasControlChars(msg.ID) || strings.TrimSpace(msg.RecipientID) == "" || len(msg.RecipientID) > 128 || hasControlChars(msg.RecipientID) || (msg.Body == "" && len(msg.AttachmentIDs) == 0) || len(msg.AttachmentIDs) > maxAttachmentsPerMessage {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: "message id, recipient_id and encrypted body or attachments are required; max 10 attachments"}))
					continue
				}
				if err := validateDirectE2EEnvelope(msg.Body, deviceID, msg.RecipientID, msg.ID); err != nil {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: err.Error()}))
					continue
				}
				if msg.SenderID != "" && msg.SenderID != deviceID {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: "sender_id does not match connection"}))
					continue
				}
				if msg.RecipientID == deviceID || !a.deviceExists(msg.RecipientID) {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: "recipient device not found"}))
					continue
				}
				if !a.allowMessage(deviceID) {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: "message rate limit exceeded"}))
					continue
				}
				m, existing, err := a.insertMessage(deviceID, msg.RecipientID, msg.Body, msg.ID, msg.AttachmentIDs)
				if err != nil {
					_ = client.WriteText(mustJSON(WSMessage{Type: "error", ID: msg.ID, Error: err.Error()}))
					continue
				}
				ack := WSMessage{Type: "message_ack", ID: m.ID, SenderID: m.SenderID, RecipientID: m.RecipientID, Body: m.Body, CreatedAt: m.CreatedAt, Status: m.Status, ClientMessageID: msg.ID, ServerSeq: m.ServerSeq, Attachments: m.Attachments}
				_ = client.WriteText(mustJSON(ack))
				if !existing {
					a.hub.SendTo(msg.RecipientID, mustJSON(WSMessage{Type: "message", ID: m.ID, SenderID: m.SenderID, RecipientID: m.RecipientID, Body: m.Body, CreatedAt: m.CreatedAt, Status: m.Status, ServerSeq: m.ServerSeq, Attachments: m.Attachments}))
				}
			default:
				_ = client.WriteText(mustJSON(WSMessage{Type: "error", Error: fmt.Sprintf("unsupported message type %q", msg.Type)}))
			}
		case ws.PingFrame:
			_ = client.WritePong(payload)
		case ws.CloseFrame:
			_ = client.WriteClose()
			return
		case ws.PongFrame:
		default:
			_ = client.WriteText(mustJSON(WSMessage{Type: "error", Error: "unsupported websocket opcode"}))
		}
	}
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func headerHasToken(values []string, want string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	}
	return false
}

func validWebSocketKey(value string) bool {
	b, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(b) == 16
}
