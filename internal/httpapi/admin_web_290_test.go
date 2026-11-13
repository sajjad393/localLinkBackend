package httpapi

import (
	"strings"
	"testing"
)

// TODO 290: every admin level has its own crown colour (owner gold, operator
// blue, security red, auditor silver), the photo ring matches, and the crown
// is the lower "round soft" crown C (no pearls).
func TestAdminWeb290CrownPerLevel(t *testing.T) {
	read := func(name string) string {
		data, err := adminWebFS.ReadFile("adminweb/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	views, app, comps, css := read("views.js"), read("app.js"), read("components.js"), read("styles.css")

	for _, want := range []string{"const CROWN_290 = {", "owner: { id: 'cr217'", "operator: { id: 'cr217-operator'", "#60A5FA",
		"security: { id: 'cr217-security'", "#F87171", "auditor: { id: 'cr217-auditor'", "#D5DCE6",
		"function crownRole290(role)", "function crown216(width, role)",
		"M9,42 L6,17 Q6,11 11,14 L29,27 L45,7 Q50,1 55,7 L71,27 L89,14 Q94,11 94,17 L91,42 Z",
		`viewBox="-6 -2 112 53"`, "is-admin-${role}", "crown216(size * 0.55, role)", "crownRole290, mix217"} {
		if !strings.Contains(comps, want) {
			t.Fatalf("components.js misses %q", want)
		}
	}
	// Crown C has no pearls any more.
	if strings.Contains(comps, "#FFF1B0") || strings.Contains(comps, "<polygon points=\"10,71.5") {
		t.Fatal("the old 217 crown with pearls is still drawn")
	}
	for _, want := range []string{".av216.is-admin-operator", "#3b82f6", ".av216.is-admin-security", "#ef4444", ".av216.is-admin-auditor", "#a8b3c2",
		".av216.is-admin { box-shadow: 0 0 0 1px var(--background), 0 0 0 2.6px #e7b416; }"} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css misses %q", want)
		}
	}
	for _, want := range []string{"LEVEL_TONE_290 = { owner: 'gold', operator: 'blue', security: 'red', auditor: 'grey' }",
		"LEVEL_TONE_290[ui.crownRole290(u.admin_role)]", "ui.crown216(12, value)", "admin: u.admin_role || false", "admin: n.admin_role || false", "admin: s.role || true"} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
	if strings.Contains(views, "admin: !!u.admin_role") || strings.Contains(views, "admin: !!n.admin_role") {
		t.Fatal("a photo still drops the admin level")
	}
	for _, want := range []string{"admin: session.role || true", "ui.crown216(15, session.role)"} {
		if !strings.Contains(app, want) {
			t.Fatalf("app.js misses %q", want)
		}
	}
}
