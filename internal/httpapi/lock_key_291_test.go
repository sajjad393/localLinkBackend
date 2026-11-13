package httpapi

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Bundle 291 (TODO_291 A): a chat request to phones that never sent their
// lock key says so in plain words; once the app PUTs its key (as 3.0.92 does
// by itself) the same request reaches that phone.
func TestChatRequestNeedsLockKeyThenWorks291(t *testing.T) {
	s := setup212(t)
	in := map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID, "range": "full"}

	res := call221(t, s.api.AdminChatRequestCreate, http.MethodPost, "/api/v1/admin/chat-requests", "", in, owner221(s))
	if res.Code != http.StatusConflict {
		t.Fatalf("no keys -> 409, got %d %s", res.Code, res.Body.String())
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &e)
	if e.Error != noLockKeyText291 || !strings.Contains(e.Error, "by themselves") || strings.Contains(e.Error, "LocalLink") {
		t.Fatalf("plain words with LinkUp, got %q", e.Error)
	}

	// The phone sends its key the way the app does: PUT /api/v1/device-key.
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := b64url(k.PublicKey().Bytes())
	put := call221(t, s.api.PutIdentityKey, http.MethodPut, "/api/v1/device-key", "", map[string]any{"public_key": key}, directoryHeaders(s.ali))
	if put.Code != http.StatusOK {
		t.Fatalf("put key %d %s", put.Code, put.Body.String())
	}
	// Same key again = 200 (the app may send it after every restart).
	if again := call221(t, s.api.PutIdentityKey, http.MethodPut, "/api/v1/device-key", "", map[string]any{"public_key": key}, directoryHeaders(s.ali)); again.Code != http.StatusOK {
		t.Fatalf("same key again %d", again.Code)
	}
	// Another key = 409, the bound key stays (only rotate may change it).
	k2, _ := ecdh.X25519().GenerateKey(rand.Reader)
	if other := call221(t, s.api.PutIdentityKey, http.MethodPut, "/api/v1/device-key", "", map[string]any{"public_key": b64url(k2.PublicKey().Bytes())}, directoryHeaders(s.ali)); other.Code != http.StatusConflict {
		t.Fatalf("other key must be 409, got %d", other.Code)
	}
	if got := s.api.identityKeyFor(s.ali.Device.ID); got != key {
		t.Fatal("the first key must stay bound")
	}

	_, card := newChatRequest221(t, s, in)
	if len(card.To) != 1 || card.Need != 1 {
		t.Fatalf("card must reach the phone with a key: boxes=%d need=%d", len(card.To), card.Need)
	}
	plain := openMine220(t, phone220{auth: s.ali, box: k.Bytes()}, card)
	if plain.Device != s.ali.Device.ID {
		t.Fatalf("box opens for ali's phone, got %q", plain.Device)
	}
}
