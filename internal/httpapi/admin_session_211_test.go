package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Bundle 211: an admin sign-in stays until logout, a revoke, or a password
// reset. It no longer ends by itself after 30 minutes.

func adminLogin211(t *testing.T, api *API, ip string) AdminLoginResponse {
	t.Helper()
	res := doJSON(t, api.AdminLogin, http.MethodPost, "/api/v1/admin/login",
		map[string]string{"username": "test-admin", "password": "test-admin-password-123"},
		map[string]string{"X-Forwarded-For": ip})
	if res.Code != http.StatusOK {
		t.Fatalf("login status %d body %s", res.Code, res.Body.String())
	}
	var out AdminLoginResponse
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func adminSummary211(t *testing.T, api *API, token string) int {
	t.Helper()
	return doJSON(t, api.AdminSummary, http.MethodGet, "/api/v1/admin/summary", nil,
		map[string]string{"Authorization": "Bearer " + token}).Code
}

func TestAdminSessionLastsUntilSignOut211(t *testing.T) {
	if adminSessionTTL < 365*24*time.Hour {
		t.Fatalf("admin sign-in must not end by itself soon, ttl=%s", adminSessionTTL)
	}
	_, api := newTestAPI(t)
	// LOCALLINK_ENV=test skips the bootstrap; make the owner account here.
	if err := api.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	login := adminLogin211(t, api, "10.9.0.1")
	exp, err := time.Parse(time.RFC3339Nano, login.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Before(time.Now().Add(365 * 24 * time.Hour)) {
		t.Fatalf("expires_at too soon: %s", login.ExpiresAt)
	}
	if code := adminSummary211(t, api, login.Token); code != http.StatusOK {
		t.Fatalf("summary with a fresh token: %d", code)
	}
	// Logout ends it.
	res := doJSON(t, api.AdminLogout, http.MethodPost, "/api/v1/admin/logout", nil,
		map[string]string{"Authorization": "Bearer " + login.Token})
	if res.Code != http.StatusOK {
		t.Fatalf("logout %d", res.Code)
	}
	if code := adminSummary211(t, api, login.Token); code != http.StatusUnauthorized {
		t.Fatalf("after logout want 401, got %d", code)
	}
}

func TestAdminSessionEndsOnRevokeAndReset211(t *testing.T) {
	_, api := newTestAPI(t)
	// LOCALLINK_ENV=test skips the bootstrap; make the owner account here.
	if err := api.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	a := adminLogin211(t, api, "10.9.0.2")
	b := adminLogin211(t, api, "10.9.0.3")
	var bID string
	if err := api.store.DB().QueryRow(`SELECT id FROM admin_sessions WHERE token_hash=?`, tokenHash(b.Token)).Scan(&bID); err != nil {
		t.Fatal(err)
	}
	res := doJSON(t, api.AdminRevokeAdminSession, http.MethodPost, "/api/v1/admin/admin-session/revoke?id="+bID, nil,
		map[string]string{"Authorization": "Bearer " + a.Token})
	if res.Code != http.StatusOK {
		t.Fatalf("revoke %d %s", res.Code, res.Body.String())
	}
	if code := adminSummary211(t, api, b.Token); code != http.StatusUnauthorized {
		t.Fatalf("revoked session want 401, got %d", code)
	}
	if code := adminSummary211(t, api, a.Token); code != http.StatusOK {
		t.Fatalf("other session must stay, got %d", code)
	}
	if err := api.ResetAdminCredentials(); err != nil {
		t.Fatal(err)
	}
	if code := adminSummary211(t, api, a.Token); code != http.StatusUnauthorized {
		t.Fatalf("after password reset want 401, got %d", code)
	}
}
