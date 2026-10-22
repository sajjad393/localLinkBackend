package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const pw = "correct-horse-battery"

func registerUser(t *testing.T, a *API, username, deviceID string) AuthResponse {
	t.Helper()
	res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": username, "password": pw, "device_id": deviceID, "device_name": deviceID + " name", "platform": "android",
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("register %s: %d %s", username, res.Code, res.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(res.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	return auth
}

func loginUser(t *testing.T, a *API, username, deviceID, fp string) (int, []byte) {
	t.Helper()
	res := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": username, "password": pw, "device_id": deviceID, "device_name": deviceID + " name", "platform": "android",
		"hardware_fp": fp,
	})
	return res.Code, res.Body.Bytes()
}

// Reinstalling the app regenerates the per-install device id, but the
// hardware fingerprint must re-bind the login to the existing device row
// instead of registering a duplicate.
func TestLoginHardwareFingerprintRebindsAfterReinstall(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	auth := registerUser(t, a, "rebinder", "phone-v1")
	userID := auth.Account.ID

	code, _ := loginUser(t, a, "rebinder", "phone-v1", "fp-DEADBEEF")
	if code != http.StatusOK {
		t.Fatalf("first login with fp: %d", code)
	}

	// "Reinstall": new random device id, same hardware fingerprint.
	code, raw := loginUser(t, a, "rebinder", "rand-abc123", "fp-DEADBEEF")
	if code != http.StatusOK {
		t.Fatalf("reinstall login: %d %s", code, raw)
	}
	var authNew AuthResponse
	if err := json.Unmarshal(raw, &authNew); err != nil {
		t.Fatal(err)
	}
	if authNew.Device.ID != "phone-v1" {
		t.Fatalf("expected bind to original device id phone-v1, got %s", authNew.Device.ID)
	}
	var n int
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM devices WHERE user_id=?`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one device row after reinstall, got %d", n)
	}

	// A genuinely new phone (different fingerprint) must remain a separate,
	// legitimate device.
	code, raw = loginUser(t, a, "rebinder", "phone-v2", "fp-OTHERDEVICE")
	if code != http.StatusOK {
		t.Fatalf("second phone login: %d %s", code, raw)
	}
	var authSecond AuthResponse
	if err := json.Unmarshal(raw, &authSecond); err != nil {
		t.Fatal(err)
	}
	if authSecond.Device.ID != "phone-v2" {
		t.Fatalf("expected new device phone-v2, got %s", authSecond.Device.ID)
	}
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM devices WHERE user_id=?`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected two legitimate devices, got %d", n)
	}

	// A foreign account must not steal the hardware identity of another user.
	registerUser(t, a, "neighbor", "n-phone")
	code, raw = loginUser(t, a, "neighbor", "n-ephemeral", "fp-DEADBEEF")
	if code != http.StatusOK {
		t.Fatalf("neighbor login: %d %s", code, raw)
	}
	var authN AuthResponse
	if err := json.Unmarshal(raw, &authN); err != nil {
		t.Fatal(err)
	}
	if authN.Device.ID != "n-ephemeral" {
		t.Fatalf("cross-account hardware fp must not rebind, got %s", authN.Device.ID)
	}
}

func TestAccountContactsCRUDIdempotentAndRestored(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	authA := registerUser(t, a, "contacts_a", "ca-phone")
	authB := registerUser(t, a, "contacts_b", "cb-phone")
	h := map[string]string{"Authorization": "Bearer " + authA.Token, "X-Device-ID": "ca-phone"}

	// Adding the same contact twice must not duplicate it.
	for i := 0; i < 2; i++ {
		res := doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", map[string]any{"user_id": authB.Account.ID}, h)
		if res.Code != http.StatusOK {
			t.Fatalf("add contact (run %d): %d %s", i, res.Code, res.Body.String())
		}
	}
	list := doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, h)
	if list.Code != http.StatusOK {
		t.Fatalf("list contacts: %d %s", list.Code, list.Body.String())
	}
	var payload struct {
		Contacts []AccountContact `json:"contacts"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Contacts) != 1 || payload.Contacts[0].UserID != authB.Account.ID {
		t.Fatalf("unexpected contacts: %+v", payload.Contacts)
	}
	if payload.Contacts[0].DeviceID == "" {
		t.Fatalf("contact device id should resolve live: %+v", payload.Contacts[0])
	}

	// Contacts appear in the restore manifest and metadata so a reinstall can
	// rebuild the local contact list.
	manifest := doJSON(t, a.GetAccountRestoreManifest, http.MethodGet, "/api/v1/account/restore/manifest", nil, h)
	var m AccountRestoreManifest
	if err := json.Unmarshal(manifest.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if manifest.Code != http.StatusOK || m.Counts["contacts"] != 1 {
		t.Fatalf("manifest contacts count: %d %+v", manifest.Code, m.Counts)
	}
	meta := doJSON(t, a.GetAccountRestoreData, http.MethodGet, "/api/v1/account/restore/data?scope=metadata", nil, h)
	if meta.Code != http.StatusOK {
		t.Fatalf("restore metadata: %d %s", meta.Code, meta.Body.String())
	}
	var page AccountRestoreData
	if err := json.Unmarshal(meta.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Contacts) != 1 || page.Contacts[0].UserID != authB.Account.ID {
		t.Fatalf("restore metadata contacts: %+v", page.Contacts)
	}

	// Self-contact and unknown users are rejected.
	self := doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", map[string]any{"user_id": authA.Account.ID}, h)
	if self.Code != http.StatusBadRequest {
		t.Fatalf("self contact should be rejected: %d", self.Code)
	}
	ghost := doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", map[string]any{"user_id": "no-such-user"}, h)
	if ghost.Code != http.StatusNotFound {
		t.Fatalf("unknown contact should 404: %d", ghost.Code)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/account/contacts/"+authB.Account.ID, nil)
	req.SetPathValue("user_id", authB.Account.ID)
	for k, v := range h {
		req.Header.Set(k, v)
	}
	del := httptest.NewRecorder()
	a.RemoveAccountContact(del, req)
	if del.Code != http.StatusOK {
		t.Fatalf("remove contact: %d %s", del.Code, del.Body.String())
	}
	list = doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, h)
	payload = struct {
		Contacts []AccountContact `json:"contacts"`
	}{}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Contacts) != 0 {
		t.Fatalf("contacts after removal: %+v", payload.Contacts)
	}
}
