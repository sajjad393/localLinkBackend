package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// Bundle 216i: redesigned web panel (Roboto from this server, Map page,
// photos through the admin avatar route, sidebar groups).
func TestAdminWeb216MapPhotosAndFonts(t *testing.T) {
	read := func(name string) string {
		data, err := adminWebFS.ReadFile("adminweb/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	views, app, comps, css := read("views.js"), read("app.js"), read("components.js"), read("styles.css")
	for _, want := range []string{"/api/v1/admin/link-map", "id: 'map'", "Who is linked to whom, live",
		"Click a person to see their phones.", "group: 'People'", "group: 'Network'", "group: 'Safety'", "group: 'System'",
		"LL.views = { dashboard, users, devices, sessions, map, network,", "No one found? Try another name or filter.",
		"Sign out all phones", "Always on", "Always off", "Blocked \\u00b7 relays only"} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
	for _, want := range []string{"/api/v1/admin/users/${encodeURIComponent(id)}/avatar", "data-avatar-user", "data-admin-pick", "data-wifi-rule"} {
		if !strings.Contains(app, want) {
			t.Fatalf("app.js misses %q", want)
		}
	}
	for _, want := range []string{"function crown216", "#FFC727", "#EFA300", "#A87400", "#E5484D", "data-avatar-user"} {
		if !strings.Contains(comps, want) {
			t.Fatalf("components.js misses %q", want)
		}
	}
	if !strings.Contains(css, `url("/admin/fonts/Roboto-SemiBold.ttf")`) || !strings.Contains(css, `"Roboto216"`) {
		t.Fatal("styles.css must load Roboto216 from this server")
	}
	// Transfers never show a file name on the web either.
	start := strings.Index(views, "const transfers = {")
	end := strings.Index(views, "function securityRows216")
	if start < 0 || end < start {
		t.Fatal("transfers page not found")
	}
	if section := views[start:end]; strings.Contains(section, "filename") || strings.Contains(section, "original_name") {
		t.Fatal("the web panel must not show file names")
	}
	_, api := newTestAPI(t)
	if res := doJSON(t, api.AdminWeb, http.MethodGet, "/admin/fonts/OFL.txt", nil); res.Code != http.StatusOK {
		t.Fatalf("font licence must be served, got %d", res.Code)
	}
}
