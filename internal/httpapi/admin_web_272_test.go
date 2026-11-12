package httpapi

import (
	"strings"
	"testing"
)

func readAdminWeb27x(t *testing.T, name string) string {
	t.Helper()
	data, err := adminWebFS.ReadFile("adminweb/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Bundle 272: blue server lines, a smaller phone-to-phone bow, no text
// selection while dragging the map, and a Network page built from real links.
func TestAdminWeb272MapAndNetworkPage(t *testing.T) {
	views, comps, css := readAdminWeb27x(t, "views.js"), readAdminWeb27x(t, "components.js"), readAdminWeb27x(t, "styles.css")
	if !strings.Contains(comps, "server: { label: 'Server', tone: 'blue', color: '#3B82C4' }") {
		t.Fatal("server lines must be blue #3B82C4")
	}
	if !strings.Contains(views, "arc217(p, q, CP, 0.25)") || !strings.Contains(views, "arc217(p, q, C, 0.60)") {
		t.Fatal("phone lines bow 0.25 around the phones; server lines keep 0.60")
	}
	at := strings.Index(css, ".map216-stage {")
	if at < 0 || !strings.Contains(css[at:at+strings.Index(css[at:], "}")], "user-select: none") {
		t.Fatal("M-2: dragging the map must not select text")
	}
	for _, want := range []string{"'Mesh links'", "Server + Wi-Fi", "'Local Wi-Fi'", "draw(el, ctx, linkData, data)"} {
		if !strings.Contains(views, want) {
			t.Fatalf("Network page misses %q", want)
		}
	}
	// Locks of 216/217 still hold.
	for _, bad := range []string{"<line x1=", `<table class="table">`, `data-map-zoom="centre"`} {
		if strings.Contains(views, bad) {
			t.Fatalf("views.js must not contain %q", bad)
		}
	}
}
