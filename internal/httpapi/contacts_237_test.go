package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Bundle 237: each contact carries past_device_ids (the person's other
// phones, newest first, max 10, never the current one, never a phone
// that now belongs to someone else).
func TestContactsPastDeviceIDs237(t *testing.T) {
	_, a := newTestAPI(t)
	alice := registerUser(t, a, "alice-237", "dev-alice")
	bob := registerUser(t, a, "bob-237", "dev-bob-new")
	carol := registerUser(t, a, "carol-237", "dev-carol")

	db := a.store.DB()
	addDevice := func(id, user, seen, status string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,status,user_id)
			VALUES(?,?,?,?,?,?,?)`, id, id, "android", seen, seen, status, user); err != nil {
			t.Fatal(err)
		}
	}
	// The current phone must be the newest active one.
	if _, err := db.Exec(`UPDATE devices SET last_seen_at='2030-01-01T00:00:00Z' WHERE id='dev-bob-new'`); err != nil {
		t.Fatal(err)
	}
	addDevice("dev-bob-old1", bob.Account.ID, "2026-01-01T00:00:00Z", "revoked")
	addDevice("dev-bob-old2", bob.Account.ID, "2026-02-01T00:00:00Z", "active")
	// A phone given to Carol (235 New owner) is Carol's now.
	addDevice("dev-was-bob", carol.Account.ID, "2026-03-01T00:00:00Z", "active")

	authed := func(tok, dev string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + tok, "X-Device-ID": dev}
	}
	res := doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts",
		map[string]any{"user_id": bob.Account.ID}, authed(alice.Token, "dev-alice"))
	if res.Code != http.StatusOK {
		t.Fatalf("add: %d %s", res.Code, res.Body.String())
	}
	list := func() map[string]AccountContact {
		t.Helper()
		res := doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, authed(alice.Token, "dev-alice"))
		if res.Code != http.StatusOK {
			t.Fatalf("list: %d", res.Code)
		}
		var out struct {
			Contacts []AccountContact `json:"contacts"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		m := map[string]AccountContact{}
		for _, c := range out.Contacts {
			m[c.UserID] = c
		}
		return m
	}
	got := list()
	b, ok := got[bob.Account.ID]
	if !ok {
		t.Fatalf("bob missing: %+v", got)
	}
	if b.DeviceID != "dev-bob-new" {
		t.Fatalf("current device = %q", b.DeviceID)
	}
	want := []string{"dev-bob-old2", "dev-bob-old1"}
	if fmt.Sprint(b.PastDeviceIDs) != fmt.Sprint(want) {
		t.Fatalf("past = %v, want %v", b.PastDeviceIDs, want)
	}
	if _, ok := got[carol.Account.ID]; ok {
		t.Fatalf("carol is not a contact")
	}

	// Max 10, newest first.
	for i := 0; i < 12; i++ {
		addDevice(fmt.Sprintf("dev-bob-x%02d", i), bob.Account.ID, fmt.Sprintf("2027-01-%02dT00:00:00Z", i+1), "revoked")
	}
	b = list()[bob.Account.ID]
	if len(b.PastDeviceIDs) != maxPastDevices237 || b.PastDeviceIDs[0] != "dev-bob-x11" {
		t.Fatalf("cap/newest: %v", b.PastDeviceIDs)
	}
	for _, id := range b.PastDeviceIDs {
		if id == "dev-bob-new" || id == "dev-was-bob" {
			t.Fatalf("must not list %s", id)
		}
	}
}

// A contact with one phone sends no past_device_ids at all (old bytes).
func TestContactsNoPastField237(t *testing.T) {
	_, a := newTestAPI(t)
	alice := registerUser(t, a, "alice-237b", "dev-alice-b")
	bob := registerUser(t, a, "bob-237b", "dev-bob-b")
	h := map[string]string{"Authorization": "Bearer " + alice.Token, "X-Device-ID": "dev-alice-b"}
	doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", map[string]any{"user_id": bob.Account.ID}, h)
	res := doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, h)
	if res.Code != http.StatusOK {
		t.Fatalf("list: %d", res.Code)
	}
	var raw struct {
		Contacts []map[string]any `json:"contacts"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Contacts) != 1 {
		t.Fatalf("contacts: %v", raw.Contacts)
	}
	if _, has := raw.Contacts[0]["past_device_ids"]; has {
		t.Fatalf("past_device_ids must be omitted when empty: %v", raw.Contacts[0])
	}
}
