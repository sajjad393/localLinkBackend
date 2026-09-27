package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"locallink/backend/internal/store"
)

func TestAccountRegistrationLoginAndLogout(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)

	register := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username":    "sajjad",
		"password":    "correct-horse-battery",
		"device_id":   "device-a",
		"device_name": "Phone A",
		"platform":    "android",
	}, nil)
	if register.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", register.Code, register.Body.String())
	}
	var reg AuthResponse
	if err := json.Unmarshal(register.Body.Bytes(), &reg); err != nil {
		t.Fatal(err)
	}
	if reg.Account.ID == "" || reg.Account.Username != "sajjad" || reg.Token == "" || reg.Device.ID != "device-a" {
		t.Fatalf("bad registration response: %+v", reg)
	}

	var storedSalt, storedHash string
	if err := st.DB().QueryRow("SELECT password_salt,password_hash FROM users WHERE id=?", reg.Account.ID).Scan(&storedSalt, &storedHash); err != nil {
		t.Fatal(err)
	}
	if storedSalt == "" || storedHash == "" || storedHash == "correct-horse-battery" || storedSalt == "correct-horse-battery" {
		t.Fatal("password appears to be stored in plaintext")
	}

	me := doJSON(t, a.AuthMe, http.MethodGet, "/api/v1/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + reg.Token,
		"X-Device-ID":   "device-a",
	})
	if me.Code != http.StatusOK || me.Body.String() == "" {
		t.Fatalf("auth me: %d %s", me.Code, me.Body.String())
	}

	loginNew := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username":    "sajjad",
		"password":    "correct-horse-battery",
		"device_id":   "device-b",
		"device_name": "Phone B",
		"platform":    "android",
	}, nil)
	if loginNew.Code != http.StatusOK {
		t.Fatalf("login new device: %d %s", loginNew.Code, loginNew.Body.String())
	}
	var login AuthResponse
	if err := json.Unmarshal(loginNew.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	if login.Token == "" || login.Device.ID != "device-b" || login.Account.ID != reg.Account.ID {
		t.Fatalf("bad new-device login: %+v", login)
	}

	badPassword := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username":    "sajjad",
		"password":    "wrong-password",
		"device_id":   "device-c",
		"device_name": "Phone C",
	}, nil)
	if badPassword.Code != http.StatusUnauthorized {
		t.Fatalf("bad password: expected 401, got %d", badPassword.Code)
	}

	loginSame := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username":    "sajjad",
		"password":    "correct-horse-battery",
		"device_id":   "device-a",
		"device_name": "Phone A",
	}, nil)
	if loginSame.Code != http.StatusOK {
		t.Fatalf("re-login existing device: %d %s", loginSame.Code, loginSame.Body.String())
	}
	var rotated AuthResponse
	if err := json.Unmarshal(loginSame.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.Token == reg.Token {
		t.Fatal("existing-device login did not rotate the session token")
	}

	oldToken := doJSON(t, a.AuthMe, http.MethodGet, "/api/v1/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + reg.Token,
		"X-Device-ID":   "device-a",
	})
	if oldToken.Code != http.StatusUnauthorized {
		t.Fatalf("old token remained valid after session rotation: %d", oldToken.Code)
	}

	logout := doJSON(t, a.LogoutAccount, http.MethodPost, "/api/v1/auth/logout", nil, map[string]string{
		"Authorization": "Bearer " + rotated.Token,
		"X-Device-ID":   "device-a",
	})
	if logout.Code != http.StatusOK {
		t.Fatalf("logout: %d %s", logout.Code, logout.Body.String())
	}
	postLogout := doJSON(t, a.AuthMe, http.MethodGet, "/api/v1/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + rotated.Token,
		"X-Device-ID":   "device-a",
	})
	if postLogout.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out token remained valid: %d", postLogout.Code)
	}
}

func TestAccountRegistrationUsernameRules(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)

	bad := []string{"ab", "space name", "-starts-wrong", "a!b"}
	for _, username := range bad {
		res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
			"username":    username,
			"password":    "strong-password",
			"device_id":   "device-" + username,
			"device_name": "Android",
		}, nil)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("username %q: expected 400, got %d (%s)", username, res.Code, res.Body.String())
		}
	}
	upper := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "UpperCase", "password": "strong-password", "device_id": "device-uppercase", "device_name": "Android",
	}, nil)
	if upper.Code != http.StatusCreated || !strings.Contains(upper.Body.String(), `"username":"uppercase"`) {
		t.Fatalf("uppercase usernames should normalize to lowercase: %d %s", upper.Code, upper.Body.String())
	}
}

func TestTrustedDeviceListAndRevocation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "trusteduser", "password": "correct-horse-battery", "device_id": "device-a", "device_name": "Phone A", "platform": "android",
	}, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	var first AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	second := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "trusteduser", "password": "correct-horse-battery", "device_id": "device-b", "device_name": "Phone B", "platform": "android",
	}, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("login second device: %d %s", second.Code, second.Body.String())
	}
	var secondAuth AuthResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondAuth); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Authorization": "Bearer " + first.Token, "X-Device-ID": "device-a"}
	list := doJSON(t, a.ListAccountDevices, http.MethodGet, "/api/v1/account/devices", nil, headers)
	if list.Code != http.StatusOK {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var page struct {
		Devices []map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	devices := page.Devices
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}
	if devices[0]["current"] != true {
		t.Fatalf("current device not first: %#v", devices[0])
	}
	revoke := doJSON(t, a.RevokeAccountDevice, http.MethodPost, "/api/v1/account/devices/revoke", map[string]any{"device_id": "device-b"}, headers)
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", revoke.Code, revoke.Body.String())
	}
	check := doJSON(t, a.AuthMe, http.MethodGet, "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer " + secondAuth.Token, "X-Device-ID": "device-b"})
	if check.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token still valid: %d %s", check.Code, check.Body.String())
	}
	var status string
	if err := st.DB().QueryRow("SELECT status FROM devices WHERE id='device-b'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "revoked" {
		t.Fatalf("expected soft revoked status, got %q", status)
	}
}

func TestRevokedDeviceCannotLoginAgain(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "revokeduser", "password": "correct-horse-battery", "device_id": "device-a", "device_name": "Phone A",
	}, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d", reg.Code)
	}
	var ar AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &ar); err != nil {
		t.Fatal(err)
	}
	revoke := doJSON(t, a.RevokeAccountDevice, http.MethodPost, "/api/v1/account/devices/revoke", map[string]any{"device_id": "device-a"}, map[string]string{"Authorization": "Bearer " + ar.Token, "X-Device-ID": "device-a"})
	if revoke.Code != http.StatusBadRequest {
		t.Fatalf("self revoke expected 400, got %d %s", revoke.Code, revoke.Body.String())
	}
	_, _ = st.DB().Exec("UPDATE devices SET status='revoked', revoked_at=CURRENT_TIMESTAMP WHERE id='device-a'")
	login := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "revokeduser", "password": "correct-horse-battery", "device_id": "device-a", "device_name": "Phone A",
	}, nil)
	if login.Code != http.StatusForbidden {
		t.Fatalf("revoked login expected 403, got %d %s", login.Code, login.Body.String())
	}
}

func TestRevokedDeviceCannotReRegisterThroughLegacyEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username":    "legacyrevoked",
		"password":    "correct-horse-battery",
		"device_id":   "device-a",
		"device_name": "Phone A",
	}, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	var ar AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &ar); err != nil {
		t.Fatal(err)
	}
	_, _ = st.DB().Exec("UPDATE devices SET status='revoked', revoked_at=CURRENT_TIMESTAMP WHERE id='device-a'")
	legacy := doJSON(t, a.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{
		"id":       "device-a",
		"name":     "Phone A",
		"platform": "android",
		"token":    ar.Token,
	}, nil)
	if legacy.Code != http.StatusForbidden {
		t.Fatalf("revoked legacy registration expected 403, got %d %s", legacy.Code, legacy.Body.String())
	}
}

func TestPairingInfoStableIdentity(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)

	one := doJSON(t, a.PairingInfo, http.MethodGet, "/api/v1/pairing/info", nil, nil)
	if one.Code != http.StatusOK {
		t.Fatalf("pairing info: %d %s", one.Code, one.Body.String())
	}
	var first PairingInfo
	if err := json.Unmarshal(one.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.ServerID == "" || len(first.Fingerprint) != 64 || first.Name == "" {
		t.Fatalf("invalid pairing info: %+v", first)
	}
	two := doJSON(t, a.PairingInfo, http.MethodGet, "/api/v1/pairing/info", nil, nil)
	var second PairingInfo
	if err := json.Unmarshal(two.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if first.ServerID != second.ServerID || first.Fingerprint != second.Fingerprint {
		t.Fatalf("server identity not stable: first=%+v second=%+v", first, second)
	}
}

func TestNewDeviceLoginRequiresPairingCodeWhenConfigured(t *testing.T) {
	t.Setenv("LOCALLINK_PAIRING_CODE", "246810")
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "pairuser", "password": "correct-horse-battery", "device_id": "device-a", "device_name": "Phone A", "pairing_code": "246810",
	}, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	missing := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "pairuser", "password": "correct-horse-battery", "device_id": "device-b", "device_name": "Phone B",
	}, nil)
	if missing.Code != http.StatusForbidden {
		t.Fatalf("missing pairing code expected 403, got %d %s", missing.Code, missing.Body.String())
	}
	valid := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "pairuser", "password": "correct-horse-battery", "device_id": "device-b", "device_name": "Phone B", "pairing_code": "246810",
	}, nil)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid pairing login: %d %s", valid.Code, valid.Body.String())
	}
}
