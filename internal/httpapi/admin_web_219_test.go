package httpapi

import (
	"strings"
	"testing"
)

// Bundle 219: after a refresh the menu button is right, the panel shows
// page-shaped shimmer instead of the login page, and only an expired login
// (401) goes back to the login page.
func TestAdminWeb219RefreshShimmerAndMenu(t *testing.T) {
	read := func(name string) string {
		data, err := adminWebFS.ReadFile("adminweb/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	views, app, comps, css, shell := read("views.js"), read("app.js"), read("components.js"), read("styles.css"), read("index.html")

	// 219a: the menu button is shown by CSS, not by a resize event.
	if strings.Contains(shell, `class="btn-icon hidden" id="menu-toggle"`) || !strings.Contains(shell, `id="menu-toggle"`) {
		t.Fatal("index.html: #menu-toggle must exist and must not start hidden")
	}
	for _, want := range []string{"#menu-toggle { display: none; }", "#menu-toggle { display: inline-flex; }", "#collapse-toggle { display: none; }"} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css misses %q", want)
		}
	}
	if strings.Contains(app, "$('menu-toggle').classList.toggle('hidden'") || strings.Contains(app, "$('collapse-toggle').classList.toggle('hidden'") {
		t.Fatal("app.js must not toggle the menu buttons on resize any more")
	}
	if !strings.Contains(app, "matchMedia('(max-width: 1000px)')") || !strings.Contains(app, "'pageshow'") {
		t.Fatal("app.js: the drawer must follow the same 1000 px rule and the pageshow event")
	}

	// 219b: shimmer from the first frame; the login page starts hidden.
	for _, want := range []string{`<main id="auth" class="auth hidden">`, `id="boot219"`, `aria-busy="true"`, `<html lang="en" data-theme="dark">`} {
		if !strings.Contains(shell, want) {
			t.Fatalf("index.html misses %q", want)
		}
	}
	if strings.Index(shell, `id="boot219"`) > strings.Index(shell, `id="auth"`) {
		t.Fatal("the boot shimmer must come before the login page")
	}
	for _, want := range []string{"function paintShell219(", "paintShell219(start);", "$('view').innerHTML = ui.bootSkeleton219(start);", "$('boot219').classList.add('hidden');"} {
		if !strings.Contains(app, want) {
			t.Fatalf("app.js misses %q", want)
		}
	}

	// 219c: a failed boot no longer drops the login.
	boot := app[strings.Index(app, "(async function boot()"):]
	boot = boot[:strings.Index(boot, "})();")]
	if strings.Contains(boot, "catch (err) {\n      showLogin();") || strings.Contains(boot, "showApp();") {
		t.Fatal("boot() must not fall back to the login page on any error")
	}
	for _, want := range []string{"AbortController", "ctrl.abort(), 10000", "Can\\'t reach the server", "Trying again in ${seconds} s…",
		"RETRY_STEPS219 = [5, 10, 20, 30]", "err.status >= 500", "signal: opts.signal", "failure.status = response.status"} {
		if !strings.Contains(app, want) {
			t.Fatalf("app.js misses %q", want)
		}
	}
	// Q1 = A: 429 keeps the login and says the server is busy.
	if !strings.Contains(app, "<strong>Server is busy</strong>") || !strings.Contains(app, "rate-limit-banner") {
		t.Fatal("app.js: the 429 bar must say Server is busy")
	}
	limit := app[strings.Index(app, "function noteRateLimit("):]
	limit = limit[:strings.Index(limit, "\n  }\n")]
	if strings.Contains(limit, "removeItem(TOKEN_KEY)") {
		t.Fatal("a 429 must never delete the saved login")
	}

	// 219d + Q2 = A: page-shaped shimmer on refresh and on page switch.
	for _, want := range []string{"function bootSkeleton219(viewId)", "sk219-page-map", "sk219-page-dashboard", "sk219-page-list", "bootSkeleton219, segmented,"} {
		if !strings.Contains(comps, want) {
			t.Fatalf("components.js misses %q", want)
		}
	}
	if !strings.Contains(app, "target.innerHTML = ui.bootSkeleton219(viewId);") || strings.Contains(app, "target.innerHTML = ui.skeletonPanel(5);") {
		t.Fatal("mountView must use the page-shaped shimmer")
	}
	for _, id := range []string{"dashboard", "users", "devices", "sessions", "security", "audit", "recovery", "map"} {
		if !strings.Contains(views, "ui.bootSkeleton219('"+id+"')") {
			t.Fatalf("views.js: %s must keep the page-shaped shimmer while it loads", id)
		}
	}
	for _, want := range []string{".boot219 {", ".sk219-map { height: 420px;", ".sk219-map { height: 300px; }", ".bar219 {", ".bar219-retry {"} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css misses %q", want)
		}
	}
	// Reduce motion still stops every shimmer (the skeleton class is shared).
	if !strings.Contains(css, ".skeleton { background: var(--surface-muted); }") {
		t.Fatal("reduce motion must keep the shimmer still")
	}
}
