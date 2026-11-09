package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Bundle 230 (audit S4): admin sign-in ends after 180 unused days, devices
// without an account are off by default, constant-time pairing compares,
// PBKDF2 600000 rounds with a quiet re-hash on login.

// oldDerivePasswordKey230 is a copy of the function before bundle 230.
func oldDerivePasswordKey230(password string, salt []byte, iterations int) []byte {
	h := func(data []byte) []byte {
		mac := hmac.New(sha256.New, []byte(password))
		_, _ = mac.Write(data)
		return mac.Sum(nil)
	}
	block := make([]byte, len(salt)+4)
	copy(block, salt)
	binary.BigEndian.PutUint32(block[len(salt):], 1)
	u := h(block)
	t := append([]byte(nil), u...)
	for i := 2; i <= iterations; i++ {
		u = h(u)
		for j := range t {
			t[j] ^= u[j]
		}
	}
	return t[:passwordKeyLength]
}

func TestFastKDFSameBytesAsOld230(t *testing.T) {
	salts := [][]byte{make([]byte, passwordSaltLength), []byte("0123456789abcdef"), bytes.Repeat([]byte{0xff}, passwordSaltLength)}
	passwords := []string{"correct-password", "", "پاس ورڈ-unicode", strings.Repeat("x", 200)}
	for _, n := range []int{1, 2, 3, 1000, 4097} {
		for _, s := range salts {
			for _, p := range passwords {
				if !bytes.Equal(derivePasswordKey(p, s, n), oldDerivePasswordKey230(p, s, n)) {
					t.Fatalf("different key for n=%d pw=%q", n, p)
				}
			}
		}
	}
	// RFC 7914 / RFC 6070-style vector for PBKDF2-HMAC-SHA256, c=1:
	// P="password", S="salt" -> 120fb6cffcf8b32c43e7225256c4f837a86548c9...
	got := derivePasswordKey("password", []byte("salt"), 1)
	want := []byte{0x12, 0x0f, 0xb6, 0xcf, 0xfc, 0xf8, 0xb3, 0x2c, 0x43, 0xe7, 0x22, 0x52, 0x56, 0xc4, 0xf8, 0x37}
	if !bytes.Equal(got[:16], want) {
		t.Fatalf("PBKDF2 vector mismatch: %x", got)
	}
}

func TestServerPasswordRoundsAre600k230(t *testing.T) {
	// TestMain lowers passwordIterations for speed; read the source value.
	src, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "var passwordIterations = 600000") {
		t.Fatal("server PBKDF2 rounds must be 600000")
	}
}

func storedRounds230(t *testing.T, a *API, table, id string) (int, string) {
	t.Helper()
	var n int
	var h string
	if err := a.store.DB().QueryRow(`SELECT password_iterations,password_hash FROM `+table+` WHERE id=?`, id).Scan(&n, &h); err != nil {
		t.Fatal(err)
	}
	return n, h
}

func makeOldHash230(t *testing.T, a *API, table, id, password string, rounds int) {
	t.Helper()
	salt := []byte("old-salt-230-xyz")
	hash := derivePasswordKey(password, salt, rounds)
	if _, err := a.store.DB().Exec(`UPDATE `+table+` SET password_salt=?,password_hash=?,password_iterations=? WHERE id=?`,
		base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(hash), rounds, id); err != nil {
		t.Fatal(err)
	}
}

func login230(t *testing.T, a *API, username, password, device string) int {
	t.Helper()
	return doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": username, "password": password, "device_id": device, "device_name": device,
	}, nil).Code
}

func TestUserLoginRehashesOldPassword230(t *testing.T) {
	_, a := newTestAPI(t)
	auth := registerDirectoryUser(t, a, "rehash230", "rh-dev-1")
	userID := auth.Account.ID
	low := passwordIterations / 2
	makeOldHash230(t, a, "users", userID, "correct-password", low)

	// A wrong password changes nothing.
	if code := login230(t, a, "rehash230", "wrong-password", "rh-dev-1"); code == http.StatusOK {
		t.Fatal("wrong password must not log in")
	}
	if n, _ := storedRounds230(t, a, "users", userID); n != low {
		t.Fatalf("wrong password must not re-hash, rounds=%d", n)
	}
	// The right password logs in and the hash moves to the new rounds.
	if code := login230(t, a, "rehash230", "correct-password", "rh-dev-1"); code != http.StatusOK {
		t.Fatalf("old hash login %d", code)
	}
	n, h1 := storedRounds230(t, a, "users", userID)
	if n != passwordIterations {
		t.Fatalf("want rounds %d after login, got %d", passwordIterations, n)
	}
	// Still logs in with the new hash, and an up to date hash is not touched.
	if code := login230(t, a, "rehash230", "correct-password", "rh-dev-1"); code != http.StatusOK {
		t.Fatalf("new hash login %d", code)
	}
	if _, h2 := storedRounds230(t, a, "users", userID); h2 != h1 {
		t.Fatal("an up to date hash must not be re-hashed again")
	}
}

func TestRehashSkipsWhenHashChangedMeanwhile230(t *testing.T) {
	_, a := newTestAPI(t)
	auth := registerDirectoryUser(t, a, "race230", "race-dev-1")
	_, current := storedRounds230(t, a, "users", auth.Account.ID)
	a.rehashPassword230("users", auth.Account.ID, "correct-password", "not-the-stored-hash")
	if _, h := storedRounds230(t, a, "users", auth.Account.ID); h != current {
		t.Fatal("re-hash must not overwrite a hash that changed meanwhile")
	}
	a.rehashPassword230("devices; DROP TABLE users;--", auth.Account.ID, "x", current) // ignored
	if _, h := storedRounds230(t, a, "users", auth.Account.ID); h != current {
		t.Fatal("unknown table must be ignored")
	}
}

func TestAdminLoginRehashesOldPassword230(t *testing.T) {
	_, a := newTestAPI(t)
	if err := a.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	var adminID string
	if err := a.store.DB().QueryRow(`SELECT id FROM admin_accounts WHERE username='test-admin'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	low := passwordIterations / 2
	makeOldHash230(t, a, "admin_accounts", adminID, "test-admin-password-123", low)
	adminLogin211(t, a, "10.23.0.1")
	if n, _ := storedRounds230(t, a, "admin_accounts", adminID); n != passwordIterations {
		t.Fatalf("admin rounds after login %d", n)
	}
	adminLogin211(t, a, "10.23.0.2") // the new hash works
}

func TestAdminSessionEndsAfter180IdleDays230(t *testing.T) {
	_, a := newTestAPI(t)
	if err := a.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	idle := adminLogin211(t, a, "10.23.1.1")
	used := adminLogin211(t, a, "10.23.1.2")
	old := time.Now().UTC().Add(-181 * 24 * time.Hour).Format(time.RFC3339Nano)
	recent := time.Now().UTC().Add(-179 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := a.store.DB().Exec(`UPDATE admin_sessions SET last_seen_at=? WHERE token_hash=?`, old, tokenHash(idle.Token)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.DB().Exec(`UPDATE admin_sessions SET last_seen_at=? WHERE token_hash=?`, recent, tokenHash(used.Token)); err != nil {
		t.Fatal(err)
	}
	if code := adminSummary211(t, a, idle.Token); code != http.StatusUnauthorized {
		t.Fatalf("181 idle days: want 401, got %d", code)
	}
	if code := adminSummary211(t, a, used.Token); code != http.StatusOK {
		t.Fatalf("179 idle days: want 200, got %d", code)
	}
	// Use renews it: last_seen_at moved to now.
	var seen string
	if err := a.store.DB().QueryRow(`SELECT last_seen_at FROM admin_sessions WHERE token_hash=?`, tokenHash(used.Token)).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if adminSessionIdleOut(seen, time.Now().UTC().Add(179*24*time.Hour)) {
		t.Fatal("a used session must renew its 180 days")
	}
	// The Sessions list shows the idle one as not active.
	res := doJSON(t, a.AdminOwnSessions, http.MethodGet, "/api/v1/admin/admin-sessions", nil,
		map[string]string{"Authorization": "Bearer " + used.Token})
	if res.Code != http.StatusOK {
		t.Fatalf("sessions list %d %s", res.Code, res.Body.String())
	}
	var list struct {
		Sessions []AdminOwnSession `json:"sessions"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	var idleShown, usedShown bool
	for _, s := range list.Sessions {
		if s.LastSeenAt == old {
			idleShown = true
			if s.Active {
				t.Fatal("idle session must show as not active")
			}
		} else if s.Current {
			usedShown = true
			if !s.Active {
				t.Fatal("current session must show as active")
			}
		}
	}
	if !idleShown || !usedShown {
		t.Fatalf("sessions missing from list: idle=%v used=%v", idleShown, usedShown)
	}
}

func TestAdminIdleOutBadTimeIsNotIdle230(t *testing.T) {
	if adminSessionIdleOut("", time.Now()) || adminSessionIdleOut("garbage", time.Now()) {
		t.Fatal("an unreadable time must not sign out")
	}
}

func TestDeviceWithoutAccountOffByDefault230(t *testing.T) {
	_, a := newTestAPI(t)
	// Made while the test environment allows it (like an old server row).
	res := doJSON(t, a.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": "solo230", "name": "solo", "platform": "android"}, nil)
	if res.Code != http.StatusCreated && res.Code != http.StatusOK {
		t.Fatalf("test env register %d %s", res.Code, res.Body.String())
	}
	var dev struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &dev); err != nil || dev.Token == "" {
		t.Fatalf("no token: %v %s", err, res.Body.String())
	}
	acct := registerDirectoryUser(t, a, "acct230", "acct-dev-230")

	soloHeaders := map[string]string{"Authorization": "Bearer " + dev.Token, "X-Device-ID": "solo230"}
	list := func(h map[string]string) int {
		return doJSON(t, a.ListDevices, http.MethodGet, "/api/v1/devices", nil, h).Code
	}

	// Real server (not test): off.
	t.Setenv("LOCALLINK_ENV", "development")
	t.Setenv("LOCALLINK_ALLOW_DEVICE_ONLY", "")
	res = doJSON(t, a.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": "solo230b", "name": "solo", "platform": "android"}, nil)
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "DEVICE_ONLY_DISABLED") {
		t.Fatalf("register without account: want 403 DEVICE_ONLY_DISABLED, got %d %s", res.Code, res.Body.String())
	}
	if code := list(soloHeaders); code != http.StatusUnauthorized {
		t.Fatalf("old device-only token: want 401, got %d", code)
	}
	if code := list(directoryHeaders(acct)); code != http.StatusOK {
		t.Fatalf("account device must keep working, got %d", code)
	}
	// Account sign up and login still work.
	registerDirectoryUser(t, a, "acct230b", "acct-dev-230b")
	if code := login230(t, a, "acct230", "correct-password", "acct-dev-230"); code != http.StatusOK {
		t.Fatalf("login %d", code)
	}

	// The owner can turn it back on.
	t.Setenv("LOCALLINK_ALLOW_DEVICE_ONLY", "true")
	if code := list(soloHeaders); code != http.StatusOK {
		t.Fatalf("allowed again: want 200, got %d", code)
	}
	res = doJSON(t, a.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": "solo230c", "name": "solo", "platform": "android"}, nil)
	if res.Code != http.StatusCreated && res.Code != http.StatusOK {
		t.Fatalf("allowed register %d %s", res.Code, res.Body.String())
	}
}

func TestPairingCodeCompareIsConstantTime230(t *testing.T) {
	for _, f := range []string{"auth.go", "api.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(src)
		if strings.Contains(s, "pairingCode != required") || strings.Contains(s, "in.PairingCode != requiredPairing") {
			t.Fatalf("%s: pairing code must use secureTokenEqual", f)
		}
	}
}
