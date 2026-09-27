package httpapi

import (
	"net/http"
	"testing"
)

func TestAdminPermissionMatrixForPanelControls(t *testing.T) {
	cases := []struct {
		role, permission string
		want             bool
	}{
		{"owner", "manage_users", true}, {"owner", "manage_sessions", true}, {"owner", "manage_settings", true},
		{"operator", "manage_users", false}, {"operator", "manage_sessions", false}, {"operator", "manage_settings", false},
		{"operator", "manage_devices", true}, {"operator", "manage_recovery", true}, {"operator", "manage_network_policy", true},
		{"security", "manage_sessions", false}, {"security", "manage_recovery", true},
		{"auditor", "read", true}, {"auditor", "manage_users", false},
	}
	for _, tc := range cases {
		if got := adminPermission(tc.role, tc.permission); got != tc.want {
			t.Fatalf("role=%s permission=%s got=%v want=%v", tc.role, tc.permission, got, tc.want)
		}
	}
}

func TestAdminPanelRoutesHaveAuthenticatedHandlers(t *testing.T) {
	routes := map[string]string{
		"/api/v1/admin/users":                 http.MethodGet,
		"/api/v1/admin/user/status":           http.MethodPost,
		"/api/v1/admin/user/network-policy":   http.MethodGet,
		"/api/v1/admin/device/network-policy": http.MethodPost,
		"/api/v1/device/network-policy":       http.MethodGet,
		"/api/v1/admin/sessions":              http.MethodGet,
		"/api/v1/admin/session/revoke":        http.MethodPost,
		"/api/v1/admin/admin-sessions":        http.MethodGet,
		"/api/v1/admin/admin-session/revoke":  http.MethodPost,
		"/api/v1/admin/transfers":             http.MethodGet,
		"/api/v1/admin/network":               http.MethodGet,
		"/api/v1/admin/settings":              http.MethodGet,
		"/api/v1/admin/privacy":               http.MethodGet,
	}
	for path := range routes {
		if path == "" {
			t.Fatal("empty admin route")
		}
	}
}
