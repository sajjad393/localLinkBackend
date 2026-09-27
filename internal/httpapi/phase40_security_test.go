package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"locallink/backend/internal/store"
)

func TestPhase40LoginLockoutAndReset(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)

	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "lockuser", "password": "correct-horse-battery", "device_id": "phone-a", "device_name": "Phone A", "platform": "android",
	}, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}

	for i := 0; i < loginFailureThreshold; i++ {
		// Keep within the per-IP/account limiter by creating a fresh API to test
		// the persistent account lock itself.
		if i > 0 {
			a = New(st)
		}
		res := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
			"username": "lockuser", "password": "wrong-password", "device_id": "dev-" + itoa(i), "device_name": "Phone", "platform": "android",
		}, nil)
		if i < loginFailureThreshold-1 && res.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d %s", i+1, res.Code, res.Body.String())
		}
		if i == loginFailureThreshold-1 && res.Code != http.StatusTooManyRequests {
			t.Fatalf("lock attempt: expected 429, got %d %s", res.Code, res.Body.String())
		}
	}

	a = New(st)
	blocked := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "lockuser", "password": "correct-horse-battery", "device_id": "phone-good", "device_name": "Phone Good", "platform": "android",
	}, nil)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("locked account should reject login, got %d %s", blocked.Code, blocked.Body.String())
	}

	_, _ = st.DB().Exec(`UPDATE users SET locked_until=? WHERE username='lockuser'`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano))
	a = New(st)
	good := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "lockuser", "password": "correct-horse-battery", "device_id": "phone-good", "device_name": "Phone Good", "platform": "android",
	}, nil)
	if good.Code != http.StatusOK {
		t.Fatalf("login after lock expiry: %d %s", good.Code, good.Body.String())
	}
	var count int
	var locked string
	if err := st.DB().QueryRow(`SELECT failed_login_count,COALESCE(locked_until,'') FROM users WHERE username='lockuser'`).Scan(&count, &locked); err != nil {
		t.Fatal(err)
	}
	if count != 0 || locked != "" {
		t.Fatalf("login failures were not reset: count=%d locked=%q", count, locked)
	}
}

func TestPhase40SecurityHeaders(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	res := doJSON(t, a.Health, http.MethodGet, "/health", nil, nil)
	for key, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
		"Cache-Control":           "no-store",
	} {
		if got := res.Header().Get(key); got != want {
			t.Fatalf("%s: got %q want %q", key, got, want)
		}
	}
}

func TestPhase40SecurityRateLimiter(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	for i := 0; i < maxAdminAttemptsPerMinute; i++ {
		if !a.allowSecurityAction("admin-ip", "10.0.0.1", maxAdminAttemptsPerMinute) {
			t.Fatalf("attempt %d unexpectedly blocked", i+1)
		}
	}
	if a.allowSecurityAction("admin-ip", "10.0.0.1", maxAdminAttemptsPerMinute) {
		t.Fatal("admin limiter failed to block extra attempt")
	}
}

func TestPhase40CleanupRemovesExpiredSecurityArtifacts(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	now := time.Now().UTC()
	_, _ = st.DB().Exec(`INSERT INTO security_audit(action,user_id,device_id,source_hash,detail,created_at) VALUES('old','','','','',?)`, now.Add(-securityAuditRetention-time.Hour).Format(time.RFC3339Nano))
	if err := a.CleanupSecurityArtifacts(); err != nil {
		t.Fatal(err)
	}
	var auditCount int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM security_audit`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 0 {
		t.Fatalf("expected old security audit to be pruned, got %d", auditCount)
	}

	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "cleanupuser", "password": "correct-horse-battery", "device_id": "cleanup-device", "device_name": "Cleanup", "platform": "android",
	}, nil)
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	var ar AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &ar); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := st.DB().Exec(`UPDATE auth_sessions SET revoked_at=? WHERE device_id=?`, old, "cleanup-device"); err != nil {
		t.Fatal(err)
	}
	if err := a.CleanupSecurityArtifacts(); err != nil {
		t.Fatal(err)
	}
	var sessions int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM auth_sessions WHERE token_hash=?`, tokenHash(ar.Token)).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("old revoked session was not pruned")
	}
}
