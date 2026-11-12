package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// Bundle 276: a phone's own default name is never a person's name.

func TestNameHelpers276(t *testing.T) {
	for _, s := range []string{"", "  ", "My Android Phone (d5d181c0)", "my android phone", "My LinkUp phone", "Phone", "LinkUp user"} {
		if !isDefaultPhoneName276(s) {
			t.Fatalf("%q must count as a default phone name", s)
		}
	}
	for _, s := range []string{"Noshad", "Sajjad's Note", "Phone 2", "Androidify"} {
		if isDefaultPhoneName276(s) {
			t.Fatalf("%q is a real name", s)
		}
	}
	if got := personName276("My Android Phone (d5d181c0)", "noshad"); got != "noshad" {
		t.Fatalf("default name -> username without @, got %q", got)
	}
	if got := personName276(" Noshad Ali ", "noshad"); got != "Noshad Ali" {
		t.Fatalf("a real name stays, got %q", got)
	}
	if got := personName276("Phone", ""); got != "LinkUp user" {
		t.Fatalf("no name, no username -> LinkUp user, got %q", got)
	}
	if got := phoneLabel276("My Android Phone (d5d181c0)", "noshad"); got != "noshad's phone" {
		t.Fatalf("phone label = <name>'s phone, got %q", got)
	}
	if got := phoneLabel276("Office Tecno", "noshad"); got != "Office Tecno" {
		t.Fatalf("a typed phone name stays, got %q", got)
	}
	if got := phoneLabel276("", "LinkUp user"); got != "Phone" {
		t.Fatalf("no person -> Phone, got %q", got)
	}
}

func profileName276(t *testing.T, a *API, userID string) string {
	t.Helper()
	var n string
	if err := a.store.DB().QueryRow(`SELECT display_name FROM profiles WHERE user_id=?`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRegisterWithDefaultPhoneNameSavesUsername276(t *testing.T) {
	_, a := newTestAPI(t)
	res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "noshad276", "password": "correct-password", "device_id": "android-276-a", "device_name": "My Android Phone (d5d181c0)",
	})
	if res.Code != http.StatusCreated && res.Code != http.StatusOK {
		t.Fatalf("register %d %s", res.Code, res.Body.String())
	}
	var uid string
	if err := a.store.DB().QueryRow(`SELECT id FROM users WHERE username='noshad276'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if got := profileName276(t, a, uid); got != "noshad276" {
		t.Fatalf("profile name = %q, want the username", got)
	}
}

func TestProfileSaveNeverStoresDefaultPhoneName276(t *testing.T) {
	_, a := newTestAPI(t)
	u := registerDirectoryUser(t, a, "ibrahim276", "android-276-b")
	res := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", map[string]any{
		"display_name": "My Android Phone (a8fe8074)", "username": "ibrahim276",
	}, directoryHeaders(u))
	if res.Code != http.StatusOK {
		t.Fatalf("update %d %s", res.Code, res.Body.String())
	}
	if got := profileName276(t, a, u.Account.ID); got != "ibrahim276" {
		t.Fatalf("profile name = %q", got)
	}
	res = doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", map[string]any{
		"display_name": "Ibrahim Khan", "username": "ibrahim276",
	}, directoryHeaders(u))
	if res.Code != http.StatusOK || profileName276(t, a, u.Account.ID) != "Ibrahim Khan" {
		t.Fatalf("a real name must be saved: %d %s", res.Code, res.Body.String())
	}
}

func TestStartupFixReplacesOnlyDefaultNames276(t *testing.T) {
	_, a := newTestAPI(t)
	bad := registerDirectoryUser(t, a, "bad276", "android-276-c")
	good := registerDirectoryUser(t, a, "good276", "android-276-d")
	db := a.store.DB()
	if _, err := db.Exec(`UPDATE profiles SET display_name='My Android Phone (c891ac8d)' WHERE user_id=?`, bad.Account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE profiles SET display_name='Good Person' WHERE user_id=?`, good.Account.ID); err != nil {
		t.Fatal(err)
	}
	a.fixDefaultProfileNames276()
	a.fixDefaultProfileNames276() // safe to repeat
	if got := profileName276(t, a, bad.Account.ID); got != "bad276" {
		t.Fatalf("default name not fixed: %q", got)
	}
	if got := profileName276(t, a, good.Account.ID); got != "Good Person" {
		t.Fatalf("real name changed: %q", got)
	}
}

func TestMemberNameSQLSkipsDefaultNames276(t *testing.T) {
	_, a := newTestAPI(t)
	u := registerDirectoryUser(t, a, "member276", "android-276-e")
	db := a.store.DB()
	if _, err := db.Exec(`UPDATE profiles SET display_name='My Android Phone (5bb3430f)' WHERE user_id=?`, u.Account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE devices SET name='My Android Phone (5bb3430f)' WHERE id=?`, u.Device.ID); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.QueryRow(`SELECT `+memberNameSQL+` FROM devices d LEFT JOIN profiles p ON p.user_id=d.user_id WHERE d.id=?`, u.Device.ID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "member276" {
		t.Fatalf("member name = %q, want the username", name)
	}
	if _, label := a.deviceUser(u.Device.ID); label != "member276's phone" {
		t.Fatalf("device label = %q", label)
	}
}

func TestAdminAnswersNeverShowDefaultPhoneNames276(t *testing.T) {
	s := newSetup216(t)
	db := s.api.store.DB()
	if _, err := db.Exec(`UPDATE devices SET name='My Android Phone (d5d181c0)'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE profiles SET display_name='My Android Phone (d5d181c0)' WHERE user_id=?`, s.bilal.Account.ID); err != nil {
		t.Fatal(err)
	}
	s.api.noteNeighbors216("dev-ali-216", []Neighbor{{"dev-bilal-216", "wifi_direct"}})
	for _, c := range []struct {
		h    http.HandlerFunc
		path string
	}{
		{s.api.AdminLinkMap, "/api/v1/admin/link-map"},
		{s.api.AdminDevices, "/api/v1/admin/devices"},
		{s.api.AdminUsers, "/api/v1/admin/users"},
		{s.api.AdminSessions, "/api/v1/admin/sessions"},
	} {
		res := doJSON(t, c.h, http.MethodGet, c.path, nil, map[string]string{"Authorization": "Bearer " + s.token})
		if res.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", c.path, res.Code, res.Body.String())
		}
		body := res.Body.String()
		if strings.Contains(body, "My Android Phone") {
			t.Fatalf("%s shows a default phone name: %s", c.path, body)
		}
	}
	m := s.linkMap(t)
	seen := false
	for _, n := range m.Nodes {
		if n.DeviceID == "dev-bilal-216" {
			seen = true
			if n.DeviceName != "bilal216's phone" {
				t.Fatalf("bilal's phone label = %q", n.DeviceName)
			}
		}
	}
	if !seen {
		t.Fatal("bilal's phone missing from the map")
	}
}
