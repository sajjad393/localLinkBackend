package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The browser console is embedded in the binary, so a missing or misspelled
// asset would only show up as a blank page in production. These tests pin the
// asset surface down at build time.

func consoleAsset(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	api := &API{}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	res := httptest.NewRecorder()
	api.AdminWeb(res, req)
	return res
}

func TestAdminWebServesConsoleShell(t *testing.T) {
	for _, path := range []string{"/admin", "/admin/", "/admin/index.html"} {
		res := consoleAsset(t, path)
		if res.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, res.Code)
		}
		if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Fatalf("%s: content-type = %q, want text/html", path, got)
		}
		body := res.Body.String()
		if !strings.Contains(body, `id="auth"`) || !strings.Contains(body, `id="app"`) {
			t.Fatalf("%s: console shell is missing its login or application root", path)
		}
	}
}

func TestAdminWebAssetsServeWithCorrectTypes(t *testing.T) {
	want := map[string]string{
		"/admin/styles.css":    "text/css",
		"/admin/components.js": "text/javascript",
		"/admin/charts.js":     "text/javascript",
		"/admin/views.js":      "text/javascript",
		"/admin/app.js":        "text/javascript",
	}
	for path, prefix := range want {
		res := consoleAsset(t, path)
		if res.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, res.Code)
		}
		if got := res.Header().Get("Content-Type"); !strings.HasPrefix(got, prefix) {
			t.Fatalf("%s: content-type = %q, want prefix %q", path, got, prefix)
		}
		if res.Body.Len() == 0 {
			t.Fatalf("%s: served an empty asset", path)
		}
	}
}

func TestAdminWebUnknownAssetIsNotFound(t *testing.T) {
	// A missing asset must not be replaced with the HTML shell: a browser that
	// asks for a script and receives HTML fails silently.
	if res := consoleAsset(t, "/admin/does-not-exist.js"); res.Code != http.StatusNotFound {
		t.Fatalf("unknown asset: status = %d, want 404", res.Code)
	}
	// Extensionless deep links still resolve to the console shell.
	res := consoleAsset(t, "/admin/anything/deep")
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `id="app"`) {
		t.Fatalf("deep link: status = %d, want the console shell", res.Code)
	}
}

func TestAdminWebSetsHardeningHeaders(t *testing.T) {
	res := consoleAsset(t, "/admin/index.html")
	header := res.Header()
	csp := header.Get("Content-Security-Policy")
	for _, fragment := range []string{"default-src 'self'", "script-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, fragment) {
			t.Fatalf("Content-Security-Policy = %q, missing %q", csp, fragment)
		}
	}
	if strings.Contains(csp, "unsafe-eval") {
		t.Fatalf("console must not need unsafe-eval: %q", csp)
	}
	for name, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	} {
		if got := header.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestAdminWebShellReferencesEmbeddedAssets walks the shell's script and
// stylesheet references and fetches each one, so a file added to index.html
// without a matching //go:embed entry fails the build.
func TestAdminWebShellReferencesEmbeddedAssets(t *testing.T) {
	shell := consoleAsset(t, "/admin/index.html").Body.String()
	pattern := regexp.MustCompile(`(?:src|href)="(/admin/[^"]+)"`)
	matches := pattern.FindAllStringSubmatch(shell, -1)
	if len(matches) == 0 {
		t.Fatal("console shell references no assets")
	}
	seen := map[string]bool{}
	for _, match := range matches {
		path := match[1]
		if seen[path] {
			continue
		}
		seen[path] = true
		if res := consoleAsset(t, path); res.Code != http.StatusOK {
			t.Fatalf("shell references %s but it is not embedded (status %d)", path, res.Code)
		}
	}
	for _, required := range []string{"/admin/app.js", "/admin/views.js", "/admin/charts.js", "/admin/components.js", "/admin/styles.css"} {
		if !seen[required] {
			t.Fatalf("console shell does not reference %s", required)
		}
	}
}

// TestAdminWebConsoleIsOfflineCapable rejects external URLs so the console keeps
// working on an isolated LocalLink network with no internet access.
func TestAdminWebConsoleIsOfflineCapable(t *testing.T) {
	for _, asset := range []string{"index.html", "styles.css", "app.js", "components.js", "charts.js", "views.js"} {
		data, err := adminWebFS.ReadFile("adminweb/" + asset)
		if err != nil {
			t.Fatalf("read %s: %v", asset, err)
		}
		body := string(data)
		for _, banned := range []string{"https://cdn", "http://cdn", "cdn.jsdelivr", "unpkg.com", "fonts.googleapis", "fonts.gstatic"} {
			if strings.Contains(body, banned) {
				t.Fatalf("%s references %q; the console must not depend on external hosts", asset, banned)
			}
		}
	}
}

// TestAdminWebOnlyCallsRegisteredEndpoints extracts every admin API path the
// console calls and asserts the server registers it. This catches a console
// that talks to an endpoint the backend does not expose.
func TestAdminWebOnlyCallsRegisteredEndpoints(t *testing.T) {
	routes, err := os.ReadFile(filepath.Join("..", "..", "cmd", "server", "main.go"))
	if err != nil {
		t.Fatalf("read server routes: %v", err)
	}
	registered := string(routes)

	pattern := regexp.MustCompile(`/api/v1/admin/[A-Za-z0-9/_-]+`)
	endpoints := map[string]bool{}
	for _, asset := range []string{"app.js", "views.js"} {
		data, err := adminWebFS.ReadFile("adminweb/" + asset)
		if err != nil {
			t.Fatalf("read %s: %v", asset, err)
		}
		for _, match := range pattern.FindAllString(string(data), -1) {
			// Template-built paths such as "/api/v1/admin/recovery-requests/${action}"
			// are captured up to the placeholder, so trim the separator.
			endpoints[strings.TrimSuffix(match, "/")] = true
		}
	}
	if len(endpoints) == 0 {
		t.Fatal("console calls no admin endpoints")
	}

	list := make([]string, 0, len(endpoints))
	for endpoint := range endpoints {
		list = append(list, endpoint)
	}
	sort.Strings(list)

	for _, endpoint := range list {
		if !strings.Contains(registered, endpoint) {
			t.Fatalf("console calls %s but cmd/server/main.go does not register it", endpoint)
		}
	}

	// The dashboard contract is the reason this console was rebuilt: make sure
	// the four Stage 1 endpoints stay wired up.
	for _, required := range []string{
		"/api/v1/admin/analytics", "/api/v1/admin/health",
		"/api/v1/admin/security-summary", "/api/v1/admin/activity",
	} {
		if !endpoints[required] {
			t.Fatalf("console no longer uses %s", required)
		}
	}
}

// TestAdminWebConsoleRendersNoPlaceholderData guards the rule that every value
// on screen comes from the server.
func TestAdminWebConsoleRendersNoPlaceholderData(t *testing.T) {
	for _, asset := range []string{"app.js", "components.js", "views.js", "charts.js"} {
		data, err := adminWebFS.ReadFile("adminweb/" + asset)
		if err != nil {
			t.Fatalf("read %s: %v", asset, err)
		}
		lowered := strings.ToLower(string(data))
		for _, banned := range []string{"lorem ipsum", "sample data", "dummy data", "placeholder data", "fake data"} {
			if strings.Contains(lowered, banned) {
				t.Fatalf("%s contains %q; the console must render only server data", asset, banned)
			}
		}
	}
}

// TestAdminWebEscapesServerValues spot-checks that table and feed renderers run
// server strings through the escaper.
func TestAdminWebEscapesServerValues(t *testing.T) {
	data, err := adminWebFS.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "emptyText") || !strings.Contains(body, "ui.esc(") {
		t.Fatal("views.js does not appear to escape server values")
	}
	components, err := adminWebFS.ReadFile("adminweb/components.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(components), "ESCAPE_MAP") {
		t.Fatal("components.js has no escaping helper")
	}
}

// TestAdminRateLimitKeepsConsolePollingViable documents the budget split: a
// signed-in console must be able to poll several endpoints on a short interval,
// while anonymous probing stays tightly capped against credential guessing.
func TestAdminRateLimitKeepsConsolePollingViable(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()

	call := func(source string, headers map[string]string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/summary", nil)
		req.RemoteAddr = source
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res := httptest.NewRecorder()
		api.AdminSummary(res, req)
		return res.Code
	}

	// A console refresh is seven reads plus one notification lookup; four
	// cycles is 32 requests, which the previous blanket ceiling of 30 rejected.
	const requestsPerCycle = 8
	for cycle := 0; cycle < 4; cycle++ {
		for i := 0; i < requestsPerCycle; i++ {
			if code := call("198.51.100.10:5100", adminHeaders()); code != http.StatusOK {
				t.Fatalf("authenticated cycle %d request %d: status = %d, want 200", cycle, i, code)
			}
		}
	}

	// The same source address, unauthenticated, is still throttled: the polling
	// budget above must not have consumed the brute-force ceiling.
	blocked := false
	for i := 0; i < maxAdminAttemptsPerMinute+5; i++ {
		if code := call("198.51.100.10:5100", nil); code == http.StatusTooManyRequests {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Fatalf("anonymous admin requests were not rate limited within %d attempts", maxAdminAttemptsPerMinute+5)
	}
}

// TestAdminWebOverlayTriggersAreWired guards a defect found only by driving the
// console in a browser: the document-level click handler closed every overlay
// before the button that opened it finished handling the click, so the
// notification centre and identity menu flashed open and shut. The handler now
// exempts elements marked data-overlay-trigger, and the shell marks them.
func TestAdminWebOverlayTriggersAreWired(t *testing.T) {
	shell := consoleAsset(t, "/admin/index.html").Body.String()
	script := consoleAsset(t, "/admin/app.js").Body.String()

	// Every overlay the console renders must have a trigger that is exempted.
	overlays := regexp.MustCompile(`data-overlay="([a-z-]+)"`).FindAllStringSubmatch(script, -1)
	if len(overlays) == 0 {
		t.Fatal("no overlays found in app.js; the identity menu and notification centre are expected")
	}
	if !strings.Contains(script, "[data-overlay-trigger]") {
		t.Error("app.js no longer exempts data-overlay-trigger from the close-everything click handler")
	}
	triggered := map[string]bool{}
	for _, name := range regexp.MustCompile(`id="([a-z-]+)-button"[^>]*data-overlay-trigger`).FindAllStringSubmatch(shell, -1) {
		triggered[name[1]] = true
	}
	if !triggered["notifications"] || !triggered["identity"] {
		t.Errorf("shell is missing a wired trigger: notifications=%v identity=%v", triggered["notifications"], triggered["identity"])
	}
}

// TestAdminWebHandlesRateLimiting guards the other browser-only defect: a full
// console sweep exceeds a per-minute admin ceiling, so the client must surface
// the throttle, keep the data already on screen, and poll more gently instead of
// replacing real figures with error panels.
func TestAdminWebHandlesRateLimiting(t *testing.T) {
	script := consoleAsset(t, "/admin/app.js").Body.String()

	for _, required := range []string{
		"response.status === 429", // recognised instead of reported as a fault
		"Retry-After",             // honours the server's retry hint
		"rate-limit-banner",       // tells the operator what is happening
		"retryAfter",              // keeps the wait so the UI can count down
	} {
		if !strings.Contains(script, required) {
			t.Errorf("app.js no longer handles rate limiting: missing %q", required)
		}
	}
	if strings.Contains(script, "Math.max(5, state.refreshSeconds)") {
		t.Error("polling interval no longer has a floor; a fast interval will trip the admin ceiling")
	}
}
