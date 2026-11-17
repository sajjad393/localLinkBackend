package httpapi

// Bundle 322 (W4/W5): the App version page runs in node (the real views.js)
// and the panel's CSS keeps the phone-width rules that W5 fixed.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// readFile322 is the same read the other web tests use (no embed needed).
func readFile322(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return raw
}

func TestAdminWebSmoke322(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, runErr := exec.Command(node, "testdata/appversion_322_smoke.js", "adminweb/views.js").CombinedOutput()
	if runErr != nil || !strings.Contains(string(out), "SMOKE_OK") {
		t.Fatalf("smoke 322 failed: %v\n%s", runErr, out)
	}
	if !strings.Contains(string(out), "6 server states") {
		t.Fatalf("expected 6 states in the smoke: %s", out)
	}
}

// The three max-content traps W5 found: a grid that sizes to its widest
// child pushed the whole panel sideways at 390 px.
func TestAdminWebCSS322NoMaxContentTrap(t *testing.T) {
	css := string(readFile322(t, "adminweb/styles.css"))
	for _, want := range []string{
		".content-inner { grid-template-columns: minmax(0, 1fr); }",
		".section { display: grid; gap: var(--space-lg); grid-template-columns: minmax(0, 1fr); }",
		".field { display: grid; gap: var(--space-xs); grid-template-columns: minmax(0, 1fr); }",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css must contain %q", want)
		}
	}
}

// The page must never put a whole sentence inside a nowrap badge.
func TestAdminWeb322StateLineWraps(t *testing.T) {
	views := string(readFile322(t, "adminweb/views.js"))
	if !strings.Contains(views, "data-version-state") {
		t.Fatal("the state line must have its own wrapping element")
	}
	if strings.Contains(views, "ui.badge(stateText") {
		t.Fatal("the state sentence must not be a badge (badge is nowrap)")
	}
	// The page asks the server to verify the build exists (V5).
	app := string(readFile322(t, "adminweb/app.js"))
	if !strings.Contains(app, "require_upload322") {
		t.Fatal("app.js must send require_upload322 when the build came from an upload")
	}
}
