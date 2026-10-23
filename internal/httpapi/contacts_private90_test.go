package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Bundle 90: per-contact privacy flag. Default is public (false), the flag is
// per-lister, an explicit is_private on a repeated add toggles it, and the
// reciprocal row always keeps its own independent value (still public).
func TestAccountContactPrivacyFlagRoundTrip(t *testing.T) {
	_, a := newTestAPI(t)
	alice := registerUser(t, a, "alice-p", "dev-alice")
	bob := registerUser(t, a, "bob-p", "dev-bob")

	authed := func(tok, dev string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + tok, "X-Device-ID": dev}
	}
	add := func(tok, device, target string, body map[string]any) {
		t.Helper()
		if body == nil {
			body = map[string]any{"user_id": target}
		}
		res := doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", body, authed(tok, device))
		if res.Code != http.StatusOK {
			t.Fatalf("add: %d %s", res.Code, res.Body.String())
		}
	}
	listPriv := func(tok, device, target string) (found, priv bool) {
		t.Helper()
		res := doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, authed(tok, device))
		if res.Code != http.StatusOK {
			t.Fatalf("list: %d", res.Code)
		}
		var out struct {
			Contacts []AccountContact `json:"contacts"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		for _, c := range out.Contacts {
			if c.UserID == target {
				return true, c.IsPrivate
			}
		}
		return false, false
	}

	// 1. Default on add is PUBLIC.
	add(alice.Token, "dev-alice", bob.Account.ID, nil)
	found, priv := listPriv(alice.Token, "dev-alice", bob.Account.ID)
	if !found {
		t.Fatalf("alice expected bob in her contacts")
	}
	if priv {
		t.Fatalf("fresh add must default to public (is_private=false)")
	}

	// 2. Re-add with explicit true TOGGLES without touching added_at contract.
	add(alice.Token, "dev-alice", bob.Account.ID, map[string]any{"user_id": bob.Account.ID, "is_private": true})
	_, priv = listPriv(alice.Token, "dev-alice", bob.Account.ID)
	if !priv {
		t.Fatalf("explicit is_private=true must persist")
	}

	// 3. Reciprocal row (bob -> alice) stays independently PUBLIC.
	found, bobPriv := listPriv(bob.Token, "dev-bob", alice.Account.ID)
	if !found {
		t.Fatalf("mutual add must create bob's reciprocal row")
	}
	if bobPriv {
		t.Fatalf("privacy may not leak onto the reciprocal row")
	}

	// 4. Toggling back to false sticks.
	add(alice.Token, "dev-alice", bob.Account.ID, map[string]any{"user_id": bob.Account.ID, "is_private": false})
	_, priv = listPriv(alice.Token, "dev-alice", bob.Account.ID)
	if priv {
		t.Fatalf("explicit is_private=false must clear the flag")
	}
}
