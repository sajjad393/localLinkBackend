package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"locallink/backend/internal/store"
)

func TestDeviceAuthAndConversationAuthorization(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)

	register := func(id, name string) string {
		res := doJSON(t, a.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": id, "name": name, "platform": "android"}, nil)
		if res.Code != http.StatusCreated {
			t.Fatalf("register %s: got %d: %s", id, res.Code, res.Body.String())
		}
		var payload struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Token
	}
	tokenA := register("a", "Phone A")
	tokenB := register("b", "Phone B")

	headersA := map[string]string{"Authorization": "Bearer " + tokenA, "X-Device-ID": "a", "Content-Type": "application/json"}
	msgRes := doJSON(t, a.CreateMessage, http.MethodPost, "/api/v1/messages", map[string]any{
		"id": "message-1", "sender_id": "a", "recipient_id": "b", "body": testDirectE2EEnvelope("message-1", "a", "b"),
	}, headersA)
	if msgRes.Code != http.StatusCreated {
		t.Fatalf("create message: got %d: %s", msgRes.Code, msgRes.Body.String())
	}
	if !strings.Contains(msgRes.Body.String(), `"server_seq":`) {
		t.Fatal("message response missing server_seq")
	}
	if !strings.Contains(msgRes.Body.String(), `"sender_id":"a"`) {
		t.Fatal("message response is not using documented JSON field names")
	}

	headersB := map[string]string{"Authorization": "Bearer " + tokenB, "X-Device-ID": "b"}
	list := doJSON(t, a.ListMessages, http.MethodGet, "/api/v1/messages?a=a&b=b&limit=10", nil, headersB)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "message-1") {
		t.Fatalf("conversation lookup failed: %d %s", list.Code, list.Body.String())
	}

	forbidden := doJSON(t, a.ListMessages, http.MethodGet, "/api/v1/messages?a=a&b=someone-else", nil, headersB)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden conversation lookup, got %d", forbidden.Code)
	}

	unauthorizedAck := doJSON(t, a.MessageAck, http.MethodPost, "/api/v1/messages/ack?id=message-1", nil, headersA)
	if unauthorizedAck.Code != http.StatusForbidden {
		t.Fatalf("sender should not acknowledge its own message as delivered, got %d", unauthorizedAck.Code)
	}

	// Capture the sender's cursor before the recipient acknowledges delivery.
	// This simulates a sender that goes offline after sending the message.
	senderInitial := doJSON(t, a.Sync, http.MethodGet, "/api/v1/sync", nil, headersA)
	if senderInitial.Code != http.StatusOK {
		t.Fatalf("sender initial sync failed: %d %s", senderInitial.Code, senderInitial.Body.String())
	}
	var initialPayload SyncResponse
	if err := json.Unmarshal(senderInitial.Body.Bytes(), &initialPayload); err != nil {
		t.Fatal(err)
	}
	ack := doJSON(t, a.MessageAck, http.MethodPost, "/api/v1/messages/ack?id=message-1", nil, headersB)
	if ack.Code != http.StatusOK {
		t.Fatalf("ack failed: %d %s", ack.Code, ack.Body.String())
	}
	if !strings.Contains(ack.Body.String(), `"status":"delivered"`) {
		t.Fatalf("ack response missing delivered status: %s", ack.Body.String())
	}
	if !strings.Contains(ack.Body.String(), `"body":"e2e:v2:`) {
		t.Fatalf("ack response lost encrypted body: %s", ack.Body.String())
	}
	cursor := initialPayload.NextCursor
	statusSync := doJSON(t, a.Sync, http.MethodGet, "/api/v1/sync?cursor="+cursor, nil, headersA)
	if statusSync.Code != http.StatusOK {
		t.Fatalf("delivery-status sync failed: %d %s", statusSync.Code, statusSync.Body.String())
	}
	var statusPayload SyncResponse
	if err := json.Unmarshal(statusSync.Body.Bytes(), &statusPayload); err != nil {
		t.Fatal(err)
	}
	if len(statusPayload.Messages) != 1 || statusPayload.Messages[0].Status != "delivered" {
		t.Fatalf("sender did not recover delivered status: %+v", statusPayload.Messages)
	}
}

func TestMessageBodyLimit(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	register := func(id string) string {
		res := doJSON(t, a.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": id, "name": id}, nil)
		var payload struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(res.Body.Bytes(), &payload)
		return payload.Token
	}
	token := register("a")
	_ = register("b")
	res := doJSON(t, a.CreateMessage, http.MethodPost, "/api/v1/messages", map[string]any{
		"id": "too-large", "sender_id": "a", "recipient_id": "b", "body": strings.Repeat("x", maxMessageLen+1),
	}, map[string]string{"Authorization": "Bearer " + token, "X-Device-ID": "a"})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected body limit rejection, got %d", res.Code)
	}
}
