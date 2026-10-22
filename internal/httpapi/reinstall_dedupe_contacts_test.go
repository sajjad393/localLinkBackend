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

// Bundle 88c: the app sends hardware_fp on register as well as login. The
// register endpoint must accept the field (decoder rejects unknown keys) and
// persist it so a later reinstall can re-bind.
func TestRegisterAccountAcceptsHardwareFingerprint(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "regfptest", "password": pw, "device_id": "fp-phone", "device_name": "My Android Phone (fp)", "platform": "android",
		"hardware_fp": "fp-REGISTER-1",
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("register with hardware_fp: %d %s", res.Code, res.Body.String())
	}
	var fp string
	if err := a.store.DB().QueryRow(`SELECT hardware_fp FROM devices WHERE id='fp-phone'`).Scan(&fp); err != nil {
		t.Fatal(err)
	}
	if fp != "fp-REGISTER-1" {
		t.Fatalf("hardware_fp not persisted on register path, got %q", fp)
	}
}

func TestAccountContactsMutualLifecycle(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	authA := registerUser(t, a, "mutual_a", "ma-phone")
	authB := registerUser(t, a, "mutual_b", "mb-phone")
	ha := map[string]string{"Authorization": "Bearer " + authA.Token, "X-Device-ID": "ma-phone"}
	hb := map[string]string{"Authorization": "Bearer " + authB.Token, "X-Device-ID": "mb-phone"}

	// A adds B: B must see A WITHOUT adding A back (mutual model), and the
	// reverse add must be harmless (no duplicates, still 200).
	res := doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", map[string]any{"user_id": authB.Account.ID}, ha)
	if res.Code != http.StatusOK {
		t.Fatalf("A adds B: %d %s", res.Code, res.Body.String())
	}
	listB := doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, hb)
	if listB.Code != http.StatusOK {
		t.Fatalf("list B: %d %s", listB.Code, listB.Body.String())
	}
	var pb struct {
		Contacts []AccountContact `json:"contacts"`
	}
	if err := json.Unmarshal(listB.Body.Bytes(), &pb); err != nil {
		t.Fatal(err)
	}
	if len(pb.Contacts) != 1 || pb.Contacts[0].UserID != authA.Account.ID {
		t.Fatalf("B should see A as contact without re-adding: %+v", pb.Contacts)
	}
	if pb.Contacts[0].DeviceID == "" {
		t.Fatalf("reverse contact device id unresolved: %+v", pb.Contacts[0])
	}

	// The reciprocal add (e.g. B's client retrying or an older version) is
	// idempotent: still exactly one contact on each side.
	res = doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", map[string]any{"user_id": authA.Account.ID}, hb)
	if res.Code != http.StatusOK {
		t.Fatalf("B reverse-adds A: %d %s", res.Code, res.Body.String())
	}
	listA := doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, ha)
	listB = doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, hb)
	var pa struct {
		Contacts []AccountContact `json:"contacts"`
	}
	pb.Contacts = nil
	if err := json.Unmarshal(listA.Body.Bytes(), &pa); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(listB.Body.Bytes(), &pb); err != nil {
		t.Fatal(err)
	}
	if len(pa.Contacts) != 1 || len(pb.Contacts) != 1 {
		t.Fatalf("reverse add must not duplicate: A=%+v B=%+v", pa.Contacts, pb.Contacts)
	}

	// Removal ends the relationship for BOTH sides.
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/account/contacts/"+authA.Account.ID, nil)
	req.SetPathValue("user_id", authA.Account.ID)
	for k, v := range hb {
		req.Header.Set(k, v)
	}
	del := httptest.NewRecorder()
	a.RemoveAccountContact(del, req)
	if del.Code != http.StatusOK {
		t.Fatalf("B removes A: %d %s", del.Code, del.Body.String())
	}
	listA = doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, ha)
	pa.Contacts = nil
	if err := json.Unmarshal(listA.Body.Bytes(), &pa); err != nil {
		t.Fatal(err)
	}
	if len(pa.Contacts) != 0 {
		t.Fatalf("mutual removal must clear both lists, A still has: %+v", pa.Contacts)
	}
}

func TestAccountContactsListHealsLegacyOneWayRows(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	authC := registerUser(t, a, "legacy_c", "lc-phone")
	authD := registerUser(t, a, "legacy_d", "ld-phone")
	hd := map[string]string{"Authorization": "Bearer " + authD.Token, "X-Device-ID": "ld-phone"}

	// Simulate a contact pair written before reciprocal adds: only C->D exists.
	if _, err := st.DB().Exec(`INSERT INTO account_contacts(user_id,contact_user_id,added_at) VALUES(?,?,?)`, authC.Account.ID, authD.Account.ID, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// Listing must backfill the missing reverse row and THEN return it, so a
	// legacy pair becomes visible to both accounts without any client action.
	list := doJSON(t, a.ListAccountContacts, http.MethodGet, "/api/v1/account/contacts", nil, hd)
	if list.Code != http.StatusOK {
		t.Fatalf("list D: %d %s", list.Code, list.Body.String())
	}
	var payload struct {
		Contacts []AccountContact `json:"contacts"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Contacts) != 1 || payload.Contacts[0].UserID != authC.Account.ID {
		t.Fatalf("legacy one-way row must surface for D: %+v", payload.Contacts)
	}
	var reverse int
	if err := st.DB().QueryRow(`SELECT COUNT(1) FROM account_contacts WHERE user_id=? AND contact_user_id=?`, authD.Account.ID, authC.Account.ID).Scan(&reverse); err != nil {
		t.Fatal(err)
	}
	if reverse != 1 {
		t.Fatalf("self-heal must persist the reverse row, found %d", reverse)
	}
}

func TestGroupMembersExposeProfileNicknameNotDeviceLabel(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	authA := registerUser(t, a, "group_a", "ga-phone")
	authB := registerUser(t, a, "group_b", "gb-phone")
	ha := map[string]string{"Authorization": "Bearer " + authA.Token, "X-Device-ID": "ga-phone"}
	hb := map[string]string{"Authorization": "Bearer " + authB.Token, "X-Device-ID": "gb-phone"}

	// B sets a profile nickname.
	upd := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile",
		map[string]any{"display_name": "Sana Oz", "username": "group_b"}, hb)
	if upd.Code != http.StatusOK && upd.Code != http.StatusCreated {
		t.Fatalf("update profile: %d %s", upd.Code, upd.Body.String())
	}

	// A creates a group with B's device, then lists members as both members.
	create := doJSON(t, a.CreateGroup, http.MethodPost, "/api/v1/groups",
		map[string]any{"name": "pair chat", "member_ids": []string{"gb-phone"}}, ha)
	if create.Code != http.StatusOK && create.Code != http.StatusCreated {
		t.Fatalf("create group: %d %s", create.Code, create.Body.String())
	}
	var group struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}
	for _, h := range []map[string]string{ha, hb} {
		members := doJSON(t, a.GroupMembers, http.MethodGet, "/api/v1/groups/members?group_id="+group.ID, nil, h)
		if members.Code != http.StatusOK {
			t.Fatalf("list members: %d %s", members.Code, members.Body.String())
		}
		var rows []map[string]any
		if err := json.Unmarshal(members.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, row := range rows {
			if row["device_id"] == "gb-phone" {
				found = true
				if name := row["name"]; name != "Sana Oz" {
					t.Fatalf("member name must be profile nickname, got %v", name)
				}
			}
		}
		if !found {
			t.Fatalf("member rows missing gb-phone: %+v", rows)
		}
	}
}
