package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Bundle 259 (B1): GET /api/v1/directory/device/{id} - who owns this phone.
func TestDirectoryByDevice259(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()
	alice := registerUser(t, a, "alice-259", "dev-alice-259")
	bob := registerUser(t, a, "bob-259", "dev-bob-259")
	carol := registerUser(t, a, "carol-259", "dev-carol-259")
	dave := registerUser(t, a, "dave-259", "dev-dave-259")
	db := a.store.DB()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE profiles SET phone_number='+923001234567', phone_visibility='contacts', display_name='Bob B' WHERE user_id=?`, bob.Account.ID)
	exec(`UPDATE profiles SET display_name='Carol C' WHERE user_id=?`, carol.Account.ID)
	// Bob's old phone (still his, revoked) and a newer current phone.
	exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,status,user_id) VALUES('dev-bob-old','old','android','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','revoked',?)`, bob.Account.ID)
	exec(`UPDATE devices SET last_seen_at='2030-01-01T00:00:00Z' WHERE id='dev-bob-259'`)
	// Alice and Bob are contacts (Bob added Alice: the other direction).
	exec(`INSERT INTO account_contacts(user_id,contact_user_id,added_at) VALUES(?,?,'2026-11-11T00:00:00Z')`, bob.Account.ID, alice.Account.ID)
	// Carol chatted with Alice but is not a contact.
	exec(`INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status,server_seq) VALUES('m259a','dev-carol-259','dev-alice-259','x','2026-11-11T00:00:00Z','sent',9001)`)

	get := func(auth AuthResponse, id string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/directory/device/"+id, nil)
		req.SetPathValue("id", id)
		if auth.Token != "" {
			for k, v := range directoryHeaders(auth) {
				req.Header.Set(k, v)
			}
		}
		res := httptest.NewRecorder()
		a.DirectoryByDevice(res, req)
		return res
	}
	card := func(res *httptest.ResponseRecorder) deviceOwnerProfile259 {
		t.Helper()
		var p deviceOwnerProfile259
		if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Contact -> 200 with the name, number hidden.
	res := get(alice, "dev-bob-259")
	if res.Code != http.StatusOK {
		t.Fatalf("contact: %d %s", res.Code, res.Body.String())
	}
	if p := card(res); p.UserID != bob.Account.ID || p.DisplayName != "Bob B" || p.PhoneNumber != "" || p.AskedDeviceID != "dev-bob-259" {
		t.Fatalf("contact card: %+v", p)
	}
	// The owner's old phone id -> the current card (current phone).
	res = get(alice, "dev-bob-old")
	if p := card(res); res.Code != http.StatusOK || p.UserID != bob.Account.ID || p.DeviceID != "dev-bob-259" || p.AskedDeviceID != "dev-bob-old" {
		t.Fatalf("old phone: %d %+v", res.Code, p)
	}
	// Message partner, not a contact -> 200.
	if res := get(alice, "dev-carol-259"); res.Code != http.StatusOK || card(res).DisplayName != "Carol C" {
		t.Fatalf("message partner: %d %s", res.Code, res.Body.String())
	}
	// ... and the other way round (Carol asks about Alice's phone).
	if res := get(carol, "dev-alice-259"); res.Code != http.StatusOK {
		t.Fatalf("message partner reverse: %d", res.Code)
	}
	// Stranger -> 404, body identical to an unknown phone.
	stranger := get(dave, "dev-bob-259")
	unknown := get(dave, "dev-nobody-259")
	if stranger.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound || stranger.Body.String() != unknown.Body.String() {
		t.Fatalf("stranger %d %q vs unknown %d %q", stranger.Code, stranger.Body.String(), unknown.Code, unknown.Body.String())
	}
	// My own phone -> my card.
	if res := get(alice, "dev-alice-259"); res.Code != http.StatusOK || card(res).UserID != alice.Account.ID {
		t.Fatalf("own phone: %d", res.Code)
	}
	// Directory sync off -> 404 (same rule as /directory/user/{id}).
	exec(`UPDATE profiles SET directory_sync_enabled=0 WHERE user_id=?`, bob.Account.ID)
	if res := get(alice, "dev-bob-259"); res.Code != http.StatusNotFound || res.Body.String() != unknown.Body.String() {
		t.Fatalf("sync off: %d %s", res.Code, res.Body.String())
	}
	exec(`UPDATE profiles SET directory_sync_enabled=1 WHERE user_id=?`, bob.Account.ID)
	// A phone moved to a new owner (235) -> the new owner's card, and only
	// for someone allowed to know the new owner.
	exec(`UPDATE devices SET user_id=? WHERE id='dev-bob-old'`, carol.Account.ID)
	exec(`INSERT INTO account_contacts(user_id,contact_user_id,added_at) VALUES(?,?,'2026-11-11T00:00:00Z')`, alice.Account.ID, carol.Account.ID)
	res = get(alice, "dev-bob-old")
	if p := card(res); res.Code != http.StatusOK || p.UserID != carol.Account.ID {
		t.Fatalf("moved phone: %d %+v", res.Code, p)
	}
	if res := get(dave, "dev-bob-old"); res.Code != http.StatusNotFound {
		t.Fatalf("moved phone stranger: %d", res.Code)
	}
	// No token -> 401.
	if res := get(AuthResponse{}, "dev-bob-259"); res.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.Code)
	}
}
