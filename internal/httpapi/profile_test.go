package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"locallink/backend/internal/store"
)

func TestProfileGetAndUpdate(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "profileuser", "password": "correct-password", "device_id": "profile-device", "device_name": "Phone",
	}, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	h := map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": auth.Device.ID}
	get := doJSON(t, a.GetProfile, http.MethodGet, "/api/v1/profile", nil, h)
	if get.Code != http.StatusOK {
		t.Fatalf("get profile: %d %s", get.Code, get.Body.String())
	}
	var profile Profile
	if err := json.Unmarshal(get.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Username != "profileuser" || profile.DisplayName != "Phone" {
		t.Fatalf("unexpected profile: %+v", profile)
	}

	update := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", map[string]any{
		"display_name": "Sajjad Ahmed", "username": "sajjad_profile",
	}, h)
	if update.Code != http.StatusOK {
		t.Fatalf("update profile: %d %s", update.Code, update.Body.String())
	}
	if err := json.Unmarshal(update.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Username != "sajjad_profile" || profile.DisplayName != "Sajjad Ahmed" {
		t.Fatalf("updated profile mismatch: %+v", profile)
	}
}

func TestProfileRequiresAccountAuthentication(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	res := doJSON(t, a.GetProfile, http.MethodGet, "/api/v1/profile", nil, nil)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}
