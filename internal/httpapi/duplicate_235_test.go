package httpapi

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func reg235(t *testing.T, a *API, username, deviceID, fp string) (int, map[string]any) {
	t.Helper()
	a.regRateMu.Lock()
	a.regRates = map[string]messageRate{}
	a.regRateMu.Unlock()
	res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": username, "password": "correct-password", "device_id": deviceID,
		"device_name": "Note", "platform": "android", "hardware_fp": fp,
	}, nil)
	out := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out
}

func list235(t *testing.T, a *API) []map[string]any {
	t.Helper()
	res := doJSON(t, a.AdminDuplicates235, http.MethodGet, "/api/v1/admin/duplicates", nil, adminHeaders())
	if res.Code != http.StatusOK {
		t.Fatalf("list: %d %s", res.Code, res.Body.String())
	}
	var out struct {
		Duplicates []map[string]any `json:"duplicates"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return out.Duplicates
}

func setup235(t *testing.T) *API {
	t.Helper()
	t.Setenv("LOCALLINK_ENV", "test")
	t.Setenv("LOCALLINK_ADMIN_TOKEN", "test-admin-token")
	_, a := newTestAPI(t)
	return a
}

func TestDuplicate235NewAccountOnKnownPhoneWaitsForProof(t *testing.T) {
	a := setup235(t)
	code, owner := reg235(t, a, "ali235", "android-note235", "fp-note-235")
	if code != http.StatusCreated {
		t.Fatalf("owner: %d %v", code, owner)
	}
	ownerID := owner["account"].(map[string]any)["id"].(string)
	code, out := reg235(t, a, "ali2x235", "android-note235", "fp-note-235")
	if code != http.StatusConflict || out["error_code"] != "DEVICE_TAKEN" || out["duplicate"] != true ||
		out["owner"] != "ali235" || out["owner_user_id"] != ownerID {
		t.Fatalf("duplicate answer: %d %v", code, out)
	}
	// A second try counts on the same line.
	_, _ = reg235(t, a, "ali2x235", "android-note235", "fp-note-235")
	rows := list235(t, a)
	if len(rows) != 1 || rows[0]["new_username"] != "ali2x235" || rows[0]["old_username"] != "ali235" ||
		rows[0]["tries"].(float64) != 2 || rows[0]["device_id"] != "android-note235" {
		t.Fatalf("waits: %v", rows)
	}
}

func TestDuplicate235ReinstallWithNewPhoneIdStillCounts(t *testing.T) {
	a := setup235(t)
	if code, out := reg235(t, a, "own235", "android-a235", "fp-same-235"); code != http.StatusCreated {
		t.Fatalf("owner: %d %v", code, out)
	}
	code, out := reg235(t, a, "new235", "android-b235", "fp-same-235")
	if code != http.StatusConflict || out["duplicate"] != true {
		t.Fatalf("same hardware, new phone id must wait: %d %v", code, out)
	}
	// Another phone (other hardware id) is free to make accounts.
	if code, out := reg235(t, a, "other235", "android-c235", "fp-other-235"); code != http.StatusCreated {
		t.Fatalf("other phone: %d %v", code, out)
	}
	// The same name again is a re-login case, never a duplicate.
	code, out = reg235(t, a, "own235", "android-a235", "fp-same-235")
	if out["duplicate"] == true || code == http.StatusCreated {
		t.Fatalf("same account must not be a duplicate: %d %v", code, out)
	}
	// No hardware id = old behaviour (no duplicate flag).
	code, out = reg235(t, a, "nofp235", "android-a235", "")
	if code != http.StatusConflict || out["duplicate"] == true {
		t.Fatalf("no hardware id: %d %v", code, out)
	}
}

func TestDuplicate235NewOwnerFreesThePhone(t *testing.T) {
	a := setup235(t)
	if code, out := reg235(t, a, "old235", "android-n235", "fp-n-235"); code != http.StatusCreated {
		t.Fatalf("owner: %d %v", code, out)
	}
	_, _ = reg235(t, a, "fresh235", "android-n235", "fp-n-235")
	rows := list235(t, a)
	if len(rows) != 1 {
		t.Fatalf("waits: %v", rows)
	}
	id := rows[0]["id"].(string)
	res := doJSON(t, a.AdminDuplicateNewOwner235, http.MethodPost, "/x", map[string]any{"id": id, "confirm": "someone"}, adminHeaders())
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "CONFIRM_MISMATCH") {
		t.Fatalf("wrong confirm: %d %s", res.Code, res.Body.String())
	}
	res = doJSON(t, a.AdminDuplicateNewOwner235, http.MethodPost, "/x", map[string]any{"id": id, "confirm": "@fresh235"}, adminHeaders())
	if res.Code != http.StatusOK {
		t.Fatalf("new owner: %d %s", res.Code, res.Body.String())
	}
	if rows := list235(t, a); len(rows) != 0 {
		t.Fatalf("list must be empty: %v", rows)
	}
	if code, out := reg235(t, a, "fresh235", "android-n235", "fp-n-235"); code != http.StatusCreated {
		t.Fatalf("new owner must join now: %d %v", code, out)
	}
	var n int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM users WHERE username='old235'`).Scan(&n)
	if n != 1 {
		t.Fatal("the old account must stay")
	}
	// A second click finds nothing to do.
	res = doJSON(t, a.AdminDuplicateNewOwner235, http.MethodPost, "/x", map[string]any{"id": id, "confirm": "fresh235"}, adminHeaders())
	if res.Code != http.StatusNotFound {
		t.Fatalf("twice: %d", res.Code)
	}
}

func TestDuplicate235DismissHidesLineOnly(t *testing.T) {
	a := setup235(t)
	_, _ = reg235(t, a, "keep235", "android-d235", "fp-d-235")
	_, _ = reg235(t, a, "try235", "android-d235", "fp-d-235")
	rows := list235(t, a)
	if len(rows) != 1 {
		t.Fatalf("waits: %v", rows)
	}
	res := doJSON(t, a.AdminDuplicateDismiss235, http.MethodPost, "/x", map[string]any{"id": rows[0]["id"]}, adminHeaders())
	if res.Code != http.StatusOK {
		t.Fatalf("dismiss: %d %s", res.Code, res.Body.String())
	}
	if rows := list235(t, a); len(rows) != 0 {
		t.Fatalf("hidden: %v", rows)
	}
	// Still blocked.
	if code, out := reg235(t, a, "try235", "android-d235", "fp-d-235"); code != http.StatusConflict || out["duplicate"] != true {
		t.Fatalf("block stays: %d %v", code, out)
	}
}

func TestDuplicate235AdminEndpointsNeedAdmin(t *testing.T) {
	a := setup235(t)
	if res := doJSON(t, a.AdminDuplicates235, http.MethodGet, "/x", nil, nil); res.Code == http.StatusOK {
		t.Fatal("list needs an admin")
	}
	if res := doJSON(t, a.AdminDuplicateNewOwner235, http.MethodPost, "/x", map[string]any{"id": "x", "confirm": "x"}, nil); res.Code == http.StatusOK {
		t.Fatal("new owner needs an admin")
	}
}

func TestDuplicate235ReturnNoteSignedByServer(t *testing.T) {
	a := setup235(t)
	res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "back235", "password": "correct-password", "device_id": "android-r235",
		"device_name": "Note", "platform": "android", "hardware_fp": "fp-r-235",
	}, nil)
	var auth AuthResponse
	_ = json.Unmarshal(res.Body.Bytes(), &auth)
	_, _ = reg235(t, a, "dup235", "android-r235", "fp-r-235")
	if len(list235(t, a)) != 1 {
		t.Fatal("wait expected")
	}
	if r := doJSON(t, a.ReturnNote235, http.MethodPost, "/x", map[string]any{"signing_key": "S", "identity_key": "I"}, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("needs sign-in: %d", r.Code)
	}
	r := doJSON(t, a.ReturnNote235, http.MethodPost, "/x", map[string]any{"signing_key": "SIGNKEY", "identity_key": "IDKEY"}, directoryHeaders(auth))
	if r.Code != http.StatusOK {
		t.Fatalf("note: %d %s", r.Code, r.Body.String())
	}
	var blob signedBlob
	_ = json.Unmarshal(r.Body.Bytes(), &blob)
	var body map[string]any
	_ = json.Unmarshal([]byte(blob.Body), &body)
	if body["kind"] != "account_return_235" || body["user_id"] != auth.Account.ID || body["device_id"] != "android-r235" ||
		body["signing_key"] != "SIGNKEY" || body["identity_key"] != "IDKEY" || body["via"] != "server" || body["username"] != "back235" {
		t.Fatalf("body: %v", body)
	}
	key, err := a.serverSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := unb64url(blob.Sig)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), []byte(blob.Body), sig) {
		t.Fatal("signature")
	}
	if rows := list235(t, a); len(rows) != 0 {
		t.Fatalf("proof clears the wait: %v", rows)
	}
}

func TestAdminWeb235WaitingForProofPanel(t *testing.T) {
	data, err := adminWebFS.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	views := string(data)
	for _, want := range []string{"/api/v1/admin/duplicates", "Waiting for proof", "/api/v1/admin/duplicates/new-owner",
		"/api/v1/admin/duplicates/dismiss", "New owner\\u2026", "Dismiss", "Blocked \\u00b7 waiting", "There is no undo.",
		"Type ${nu} to confirm", "Give phone to @${nu}", "ctx.can('manageUsers')", "Old account (kept)"} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
}

func TestDuplicate235LoginOfAnotherAccountOnKnownPhoneWaits(t *testing.T) {
	a := setup235(t)
	if code, out := reg235(t, a, "phone235", "android-l235", "fp-l-235"); code != http.StatusCreated {
		t.Fatalf("owner: %d %v", code, out)
	}
	if code, out := reg235(t, a, "elsewhere235", "android-x235", "fp-x-235"); code != http.StatusCreated {
		t.Fatalf("other: %d %v", code, out)
	}
	res := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "elsewhere235", "password": "correct-password", "device_id": "android-l235",
		"device_name": "Note", "platform": "android", "hardware_fp": "fp-l-235",
	}, nil)
	out := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if res.Code != http.StatusConflict || out["duplicate"] != true || out["owner"] != "phone235" {
		t.Fatalf("login on a known phone: %d %v", res.Code, out)
	}
	rows := list235(t, a)
	if len(rows) != 1 || rows[0]["via"] != "login" {
		t.Fatalf("waits: %v", rows)
	}
	// The owner itself signs in as before.
	if code, body := loginUser235(t, a, "phone235", "android-l235", "fp-l-235"); code != http.StatusOK {
		t.Fatalf("owner login: %d %s", code, body)
	}
}

func loginUser235(t *testing.T, a *API, username, deviceID, fp string) (int, string) {
	t.Helper()
	res := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": username, "password": "correct-password", "device_id": deviceID,
		"device_name": "Note", "platform": "android", "hardware_fp": fp,
	}, nil)
	return res.Code, res.Body.String()
}
