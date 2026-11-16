package httpapi

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Bundle 321 (R9): the web owner viewer follows the app's 321 messages
// screen (R4, mock messages_redesign_321.html) and the list words (R1):
// files card on top, photo / video tiles, the mic in the voice ring,
// "Ready to read" only when every file is here.
func TestAdminWebViewer321(t *testing.T) {
	raw, err := os.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	views := string(raw)
	for _, want := range []string{
		"function filesCard321(", "function tile321(", "function viewFiles321(",
		"<div class=\"vmain221\">${filesCard321()}${notes}${body}${progress221()}</div>",
		"if (hasThumb && (isImage || isVideo)) return tile321(a, st317, f, isVideo, name, size, onPhoto);",
		"Coming by itself", "Next in line", "files here</b>", "All files here",
		"`Coming \\u00b7 ${f.parts} / ${f.of}`", "<span class=\"mic321\">${MIC321}</span>",
		"Messages here \\u00b7 ${f317.here} of ${f317.wanted} files coming", "not available</span>", "Ready to read",
		"&& viewFiles321().every((x) => x.st.k === 'here');",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js must contain %q", want)
		}
	}
	// No Stop / Get button on the page: the Owner's phone brings the files.
	i := strings.Index(views, "function filesCard321(")
	j := strings.Index(views, "function tile321(")
	if i < 0 || j < i || strings.Contains(views[i:j], "data-conv") {
		t.Fatalf("the files card must not have a button")
	}
	rawCSS, _ := os.ReadFile("adminweb/styles.css")
	css := string(rawCSS)
	for _, want := range []string{".tc321 {", ".pc321 i {", ".t321 {", ".t321.wait .th221 { filter: blur(6px)", ".mid321 {", ".ring321 {", ".tm321 {", ".ft321 {", ".mic321 {"} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css must contain %q", want)
		}
	}
}

func TestAdminWebSmoke321(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/conv_321_smoke.js", "adminweb/views.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SMOKE_OK") {
		t.Fatalf("smoke 321 failed: %v\n%s", err, out)
	}
}
