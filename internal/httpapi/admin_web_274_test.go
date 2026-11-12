package httpapi

import (
	"strings"
	"testing"
)

// Bundle 274: the web panel watches /api/v1/admin/live (first-frame auth, no
// token in the URL, plain ws on http), falls back to a 15 s poll, and the
// dashboard colours come from the right CSS variables.
func TestAdminWeb274LiveSocketAndDashboard(t *testing.T) {
	app, views, css := readAdminWeb27x(t, "app.js"), readAdminWeb27x(t, "views.js"), readAdminWeb27x(t, "styles.css")
	for _, want := range []string{"/api/v1/admin/live", "type: 'auth', token: state.token", "'ws:'", "15000",
		"live: (fn) => live274.watch(fn)", "data-live274", "live274.watch(null)"} {
		if !strings.Contains(app, want) {
			t.Fatalf("app.js misses %q", want)
		}
	}
	if strings.Contains(app, "admin/live?token") || strings.Contains(app, "/admin/live' + '?") {
		t.Fatal("the admin token must never go in the live socket URL")
	}
	// D-1: '--success' must lose its dashes before ui.token adds them.
	if !strings.Contains(views, ".replace(/^--/, '')") {
		t.Fatal("cssVarToHex must strip the leading -- (all colours were blue)")
	}
	// D-2: each phone once; no "Mesh capable" tile.
	if strings.Contains(views, "Mesh capable") {
		t.Fatal("\"Mesh capable\" must be gone from the dashboard")
	}
	for _, want := range []string{"function phoneWays274", "Each phone once", "data-live274-net", "ctx.live((msg)"} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
	// The map redraws from the frame itself and keeps the phones in place.
	for _, want := range []string{"liveData ? { ok: true, data: liveData }", "mapView.pendingLive", "mapView.pos"} {
		if !strings.Contains(views, want) {
			t.Fatalf("live map misses %q", want)
		}
	}
	if !strings.Contains(css, ".live274") {
		t.Fatal("styles.css misses the Live dot")
	}
}
