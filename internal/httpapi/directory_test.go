package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func registerDirectoryUser(t *testing.T, a *API, username, deviceID string) AuthResponse {
	t.Helper()
	res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": username, "password": "correct-password", "device_id": deviceID, "device_name": deviceID,
	}, nil)
	if res.Code != http.StatusCreated {
		t.Fatalf("register %s: %d %s", username, res.Code, res.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(res.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	return auth
}

func directoryHeaders(auth AuthResponse) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + auth.Token,
		"X-Device-ID":   auth.Device.ID,
	}
}

func TestDirectoryPhoneNameAndUserLookupRespectPrivacy(t *testing.T) {
	st, a := newTestAPI(t)
	_ = st
	owner := registerDirectoryUser(t, a, "directoryowner", "directory-owner")
	requester := registerDirectoryUser(t, a, "directoryrequester", "directory-requester")
	requesterHeaders := directoryHeaders(requester)
	ownerHeaders := directoryHeaders(owner)

	update := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", map[string]any{
		"display_name": "Muhammad Z", "username": "directoryowner", "phone_number": "03001234567",
		"phone_visibility": "private", "discoverable_by_phone": true, "discoverable_by_name": true, "directory_sync_enabled": true,
	}, ownerHeaders)
	if update.Code != http.StatusOK {
		t.Fatalf("owner update: %d %s", update.Code, update.Body.String())
	}
	var updated Profile
	if err := json.Unmarshal(update.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.PhoneNumber != "+923001234567" || updated.ProfileVersion != 1 {
		t.Fatalf("unexpected updated profile: %+v", updated)
	}

	byPhone := doJSON(t, a.DirectoryByPhone, http.MethodGet, "/api/v1/directory/phone?phone=+92%20300%201234567", nil, requesterHeaders)
	if byPhone.Code != http.StatusOK {
		t.Fatalf("phone lookup: %d %s", byPhone.Code, byPhone.Body.String())
	}
	var phoneProfile Profile
	if err := json.Unmarshal(byPhone.Body.Bytes(), &phoneProfile); err != nil {
		t.Fatal(err)
	}
	if phoneProfile.UserID != owner.Account.ID || phoneProfile.DeviceID != owner.Device.ID {
		t.Fatalf("unexpected phone lookup identity: %+v", phoneProfile)
	}
	if phoneProfile.PhoneNumber != "" {
		t.Fatalf("private phone number leaked: %+v", phoneProfile)
	}

	byName := doJSON(t, a.DirectoryByName, http.MethodGet, "/api/v1/directory/name?q=Muhammad", nil, requesterHeaders)
	if byName.Code != http.StatusOK {
		t.Fatalf("name lookup: %d %s", byName.Code, byName.Body.String())
	}
	var namePayload struct {
		Profiles []Profile `json:"profiles"`
	}
	if err := json.Unmarshal(byName.Body.Bytes(), &namePayload); err != nil {
		t.Fatal(err)
	}
	if len(namePayload.Profiles) != 1 || namePayload.Profiles[0].UserID != owner.Account.ID || namePayload.Profiles[0].PhoneNumber != "" {
		t.Fatalf("unexpected name lookup result: %+v", namePayload.Profiles)
	}

	byUser := doJSON(t, a.DirectoryByUserID, http.MethodGet, "/api/v1/directory/user/"+owner.Account.ID, nil, requesterHeaders)
	if byUser.Code != http.StatusOK {
		t.Fatalf("user lookup for QR identity: %d %s", byUser.Code, byUser.Body.String())
	}
	var userProfile Profile
	if err := json.Unmarshal(byUser.Body.Bytes(), &userProfile); err != nil {
		t.Fatal(err)
	}
	if userProfile.UserID != owner.Account.ID || userProfile.PhoneNumber != "" {
		t.Fatalf("unexpected user lookup: %+v", userProfile)
	}

	private := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", map[string]any{
		"display_name": "Muhammad Z", "username": "directoryowner", "phone_number": "03001234567",
		"phone_visibility": "private", "discoverable_by_phone": false, "discoverable_by_name": false, "directory_sync_enabled": true,
	}, ownerHeaders)
	if private.Code != http.StatusOK {
		t.Fatalf("privacy update: %d %s", private.Code, private.Body.String())
	}
	phoneHidden := doJSON(t, a.DirectoryByPhone, http.MethodGet, "/api/v1/directory/phone?phone=03001234567", nil, requesterHeaders)
	if phoneHidden.Code != http.StatusNotFound {
		t.Fatalf("expected hidden phone to be undiscoverable, got %d %s", phoneHidden.Code, phoneHidden.Body.String())
	}
	nameHidden := doJSON(t, a.DirectoryByName, http.MethodGet, "/api/v1/directory/name?q=Muhammad", nil, requesterHeaders)
	if nameHidden.Code != http.StatusOK {
		t.Fatalf("hidden name query: %d %s", nameHidden.Code, nameHidden.Body.String())
	}
	var hiddenPayload struct {
		Profiles []Profile `json:"profiles"`
	}
	if err := json.Unmarshal(nameHidden.Body.Bytes(), &hiddenPayload); err != nil {
		t.Fatal(err)
	}
	if len(hiddenPayload.Profiles) != 0 {
		t.Fatalf("hidden name was still discoverable: %+v", hiddenPayload.Profiles)
	}

	self := doJSON(t, a.GetProfile, http.MethodGet, "/api/v1/profile", nil, ownerHeaders)
	if self.Code != http.StatusOK {
		t.Fatalf("self profile: %d %s", self.Code, self.Body.String())
	}

	clear := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", map[string]any{
		"display_name": "Muhammad Z", "username": "directoryowner", "phone_number": "",
	}, ownerHeaders)
	if clear.Code != http.StatusOK {
		t.Fatalf("clear phone: %d %s", clear.Code, clear.Body.String())
	}
	var cleared Profile
	if err := json.Unmarshal(clear.Body.Bytes(), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.PhoneNumber != "" {
		t.Fatalf("phone number was not cleared: %+v", cleared)
	}
}

func TestNormalizePhoneNumber(t *testing.T) {
	cases := map[string]string{
		"03001234567":      "+923001234567",
		"0300 1234567":     "+923001234567",
		"+92 300 1234567":  "+923001234567",
		"0092 300 1234567": "+923001234567",
		"+923001234567":    "+923001234567",
	}
	for input, want := range cases {
		if got := normalizePhoneNumber(input); got != want {
			t.Fatalf("normalizePhoneNumber(%q) = %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{"03001234", "not-a-phone", "923001234567x"} {
		if got := normalizePhoneNumber(input); got != "" {
			t.Fatalf("normalizePhoneNumber(%q) = %q, want empty", input, got)
		}
	}
}

func TestDirectoryRejectsDuplicatePhoneNumber(t *testing.T) {
	st, a := newTestAPI(t)
	_ = st
	first := registerDirectoryUser(t, a, "directoryfirst", "directory-first")
	second := registerDirectoryUser(t, a, "directorysecond", "directory-second")
	res := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", map[string]any{
		"display_name": "First", "username": "directoryfirst", "phone_number": "03001234567",
	}, directoryHeaders(first))
	if res.Code != http.StatusOK {
		t.Fatalf("first phone update: %d %s", res.Code, res.Body.String())
	}
	res = doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", map[string]any{
		"display_name": "Second", "username": "directorysecond", "phone_number": "+92 300 1234567",
	}, directoryHeaders(second))
	if res.Code != http.StatusConflict {
		t.Fatalf("duplicate phone expected 409, got %d %s", res.Code, res.Body.String())
	}
}
