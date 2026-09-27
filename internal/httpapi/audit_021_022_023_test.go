package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAudit023AdminRolesAndSessionPolicy(t *testing.T) {
	cases := []struct {
		role, permission string
		want             bool
	}{
		{"owner", "backup", true},
		{"owner", "manage_devices", true},
		{"owner", "manage_recovery", true},
		{"operator", "read", true},
		{"operator", "manage_devices", true},
		{"operator", "manage_recovery", true},
		{"operator", "backup", false},
		{"security", "read", true},
		{"security", "manage_recovery", true},
		{"security", "manage_devices", false},
		{"auditor", "read", true},
		{"auditor", "manage_devices", false},
		{"auditor", "backup", false},
		{"unknown", "read", false},
	}
	for _, tc := range cases {
		if got := adminPermission(tc.role, tc.permission); got != tc.want {
			t.Fatalf("role=%s permission=%s got=%v want=%v", tc.role, tc.permission, got, tc.want)
		}
	}
}

func TestAudit023StaticAdminTokenPolicy(t *testing.T) {
	if adminStaticTokenAllowed("production") || adminStaticTokenAllowed("") {
		t.Fatal("static admin tokens must not be accepted outside test/development")
	}
	if !adminStaticTokenAllowed("test") || !adminStaticTokenAllowed("development") {
		t.Fatal("test/development compatibility mode should be explicit")
	}
}

func TestAudit023AdminLoginRateLimitKeyIncludesUsername(t *testing.T) {
	t.Setenv("LOCALLINK_ENV", "test")
	keyA := authUsernameRateKey(testRequest("10.0.0.7"), "admin")
	keyB := authUsernameRateKey(testRequest("10.0.0.7"), "operator")
	if keyA == keyB {
		t.Fatal("admin login rate-limit keys must distinguish usernames")
	}
}

func testRequest(ip string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/admin/login", nil)
	r.RemoteAddr = ip + ":12345"
	return r
}
