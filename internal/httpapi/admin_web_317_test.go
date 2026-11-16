package httpapi

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Bundle 317 (TODO_317 A / B / C / D / G6 / G8): the web viewer shows the
// same as the app - system lines, the lighter deleted look, one row per
// file, the file states with no Get button, and the files under a Ready card.
func TestAdminWebViewer317(t *testing.T) {
	raw, err := os.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	views := string(raw)
	rawCSS, _ := os.ReadFile("adminweb/styles.css")
	css := string(rawCSS)
	for _, want := range []string{
		"function sysLines317(", "class=\"sys317\"", "Deleted ${ui.esc(delAt317(m.deleted_at, m.created_at))}", "seen317[key]",
		"Too big to bring (over 50 MB)", "phone any more", "Next in line", "`Coming ${f.parts} / ${f.of}`",
		// Bundle 321 (R9 = app R1): the Finished rows say "Messages here · x of y files coming" / "Ready to read".
		"function files317(", "function filesStrip317(", "Messages here \\u00b7 ${f317.here} of ${f317.wanted} files coming", "Ready to read",
		"The chat is on the way",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js must contain %q", want)
		}
	}
	// G6: no Get button on the page (the Owner's phone asks by itself).
	for _, gone := range []string{"<b>Get full ${word}", "aria-label=\"Get the voice note from the phone\"", "title=\"Get full ${word}\""} {
		if strings.Contains(views, gone) {
			t.Fatalf("views.js still has a Get button: %q", gone)
		}
	}
	for _, want := range []string{".sys317 span", ".del317", ".fs317bar", ".mini307.b"} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css must contain %q", want)
		}
	}
}

func TestAdminWebSmoke317(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/conv_317_smoke.js", "adminweb/views.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SMOKE_OK") {
		t.Fatalf("smoke 317 failed: %v\n%s", err, out)
	}
}
