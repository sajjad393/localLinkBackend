package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"locallink/backend/internal/store"
)

func testDirectE2EEnvelope(id, sender, recipient string) string {
	return directMessageEnvelope + `{"version":2,"type":"direct_message","sender_id":"` + sender + `","recipient_id":"` + recipient + `","message_id":"` + id + `","created_at":"2026-09-23T12:00:00Z","nonce":"AQEBAQEBAQEBAQEB","ciphertext":"Y2lwaGVydGV4dA","mac":"AQEBAQEBAQEBAQEBAQEBAQ"}`
}

func TestValidateDirectE2EEnvelopeRejectsPlaintextAndBindingMismatch(t *testing.T) {
	id, sender, recipient := "m-1", "a", "b"
	if err := validateDirectE2EEnvelope("hello", sender, recipient, id); err == nil {
		t.Fatal("plaintext direct message must be rejected")
	}
	valid := testDirectE2EEnvelope(id, sender, recipient)
	if err := validateDirectE2EEnvelope(valid, sender, recipient, id); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}
	for _, mutated := range []string{
		testDirectE2EEnvelope(id, "evil", recipient),
		testDirectE2EEnvelope(id, sender, "evil"),
		testDirectE2EEnvelope("other", sender, recipient),
	} {
		if err := validateDirectE2EEnvelope(mutated, sender, recipient, id); err == nil {
			t.Fatal("mutated identity binding accepted")
		}
	}
}

func TestCreateMessageRequiresE2EV2(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	register := func(id string) string {
		res := doJSON(t, a.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": id, "name": id, "platform": "android"}, nil)
		if res.Code != http.StatusCreated {
			t.Fatalf("register %s: %d %s", id, res.Code, res.Body.String())
		}
		var out struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Token
	}
	tokenA := register("a")
	_ = register("b")
	headers := map[string]string{"Authorization": "Bearer " + tokenA, "X-Device-ID": "a"}
	bad := doJSON(t, a.CreateMessage, http.MethodPost, "/api/v1/messages", map[string]any{"id": "m-plain", "recipient_id": "b", "body": "hello"}, headers)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "e2e:v2") {
		t.Fatalf("plaintext was not rejected: %d %s", bad.Code, bad.Body.String())
	}
	good := doJSON(t, a.CreateMessage, http.MethodPost, "/api/v1/messages", map[string]any{"id": "m-good", "recipient_id": "b", "body": testDirectE2EEnvelope("m-good", "a", "b")}, headers)
	if good.Code != http.StatusCreated {
		t.Fatalf("valid encrypted message rejected: %d %s", good.Code, good.Body.String())
	}
	if strings.Contains(good.Body.String(), `"body":"hello"`) {
		t.Fatal("plaintext body leaked into encrypted-message response")
	}
}

func TestRateLimitScopesRejectExcessPresenceAndCallTraffic(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	for i := 0; i < 30; i++ {
		if !a.allowPresenceUpdate("device-presence") {
			t.Fatalf("presence attempt %d unexpectedly blocked", i+1)
		}
	}
	if a.allowPresenceUpdate("device-presence") {
		t.Fatal("31st presence update should be blocked")
	}
	for i := 0; i < 60; i++ {
		if !a.allowCallHeartbeat("device-heartbeat") {
			t.Fatalf("heartbeat attempt %d unexpectedly blocked", i+1)
		}
	}
	if a.allowCallHeartbeat("device-heartbeat") {
		t.Fatal("61st heartbeat should be blocked")
	}
}

func TestMessageRateLimitIsAccountScoped(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	if _, err := st.DB().Exec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at) VALUES('u','rateuser','s','h',1,'active','now','now')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,user_id,token_hash,status) VALUES(?,?,?,?,?,?,?,?)`, id, id, "android", "now", "now", "u", strings.Repeat(id, 32), "active"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 300; i++ {
		if !a.allowPersistentSecurityAction("message-account", "u", 300, time.Minute) {
			t.Fatalf("account attempt %d unexpectedly blocked", i+1)
		}
	}
	if a.allowPersistentSecurityAction("message-account", "u", 300, time.Minute) {
		t.Fatal("account-level 301st attempt should be blocked")
	}
}
