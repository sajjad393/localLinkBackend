package httpapi

import (
	"strings"
	"testing"
)

// Bundle 217: aerial moving map lines, the new crown and a fully responsive
// web panel (phone <= 640 px, tablet 641-1000 px).
func TestAdminWeb217ResponsiveMapAndCrown(t *testing.T) {
	read := func(name string) string {
		data, err := adminWebFS.ReadFile("adminweb/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	views, app, comps, css, shell := read("views.js"), read("app.js"), read("components.js"), read("styles.css"), read("index.html")

	for _, want := range []string{".map216-world > svg", "@media (max-width: 640px)", "@media (min-width: 641px) and (max-width: 1000px)",
		"td::before", "@keyframes flow217", "calc(-100% + .4px)"} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css misses %q", want)
		}
	}
	if strings.Contains(css, ".map216-world svg {") {
		t.Fatal("the map svg rule must not hit the server icon (use .map216-world > svg)")
	}
	reduce := strings.LastIndex(css, "prefers-reduced-motion")
	if reduce < 0 || !strings.Contains(css[reduce:], ".link217 .flow") {
		t.Fatal("reduce motion must stop the moving map lines")
	}
	// true when any "selector {" rule in styles.css holds want.
	ruleHas := func(selector, want string) bool {
		for rest := css; ; {
			at := strings.Index(rest, selector+" {")
			if at < 0 {
				return false
			}
			end := strings.Index(rest[at:], "}")
			if end < 0 {
				return false
			}
			if strings.Contains(rest[at:at+end], want) {
				return true
			}
			rest = rest[at+end:]
		}
	}
	if !ruleHas(".signout216", "white-space: nowrap") {
		t.Fatal("Sign out must stay on one line")
	}
	if !strings.Contains(css, ".side216-head { position: sticky") {
		t.Fatal("the phone Manage page needs a sticky head")
	}

	for _, want := range []string{"data-label=", "col-first", "col-act", "col-status", "hide-tab", "mix217", "cr217", "#FFE985", "#3B82F6",
		"function crown216", "#FFC727", "#EFA300", "#A87400", "#E5484D"} {
		if !strings.Contains(comps, want) {
			t.Fatalf("components.js misses %q", want)
		}
	}

	for _, want := range []string{"animateMotion", `class="link217`, "fit217(", "arc217(", `data-map-zoom="fit"`, "Fit everyone",
		"hideTablet: true", "Click a person to see their phones.", "Blocked \\u00b7 relays only", "side216-back"} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
	for _, gone := range []string{"<line x1=", `<table class="table">`, `data-map-zoom="centre"`} {
		if strings.Contains(views, gone) {
			t.Fatalf("views.js still has %q", gone)
		}
	}

	if !strings.Contains(shell, `id="i-fit"`) || !strings.Contains(shell, `id="drawer-close"`) {
		t.Fatal("index.html needs the fit icon and the drawer close button")
	}

	at := strings.Index(app, "function openIdentityMenu()")
	if at < 0 {
		t.Fatal("app.js misses openIdentityMenu")
	}
	menu := app[at:]
	menu = menu[:strings.Index(menu, "\n  }\n")]
	for _, want := range []string{"avatar216(", "Refresh data", "Toggle light / dark", "Privacy & access", "Sign out"} {
		if !strings.Contains(menu, want) {
			t.Fatalf("the photo menu misses %q", want)
		}
	}
	for _, want := range []string{"$('drawer-close')", "dx < -60"} {
		if !strings.Contains(app, want) {
			t.Fatalf("app.js misses %q", want)
		}
	}
}
