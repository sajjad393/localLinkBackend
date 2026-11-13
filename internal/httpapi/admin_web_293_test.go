package httpapi

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Bundle 293 (TODO_293): the web Map on phone browsers. The map fits the
// visible screen with the Phones list peeking under it, 2 phones in a row
// without stagger, server lines stay inside the map, a short "+N more" text,
// and a little zoomed out by default (80 - 90 %). The computer view is unchanged.
func TestAdminWebMapPhone293(t *testing.T) {
	rawViews, err := os.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	rawCSS, err := os.ReadFile("adminweb/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	views, css := string(rawViews), string(rawCSS)
	for _, want := range []string{
		"const stagger = perRow >= 3;",
		"if (stagger && xs.some(",
		"if (stageW <= 640) {",
		"const wantP = Math.min(0.9,",
		"const kp = Math.max(0.8, wantP);",
		"in291.push(...edge293);",
		"const fitHeight293 = () => {",
		"if (window.innerWidth > 1100) { stage.style.height = ''; return; }",
		"window.innerHeight - top - 64",
		"fitHeight293(); if (!mapView.fitted) autoFit();",
		"more \\u00b7 drag the map to see them",
		"legendEl293.classList.toggle('one', legendCount293 < 2);",
		"b.blur();",
		// 292 stays for the computer.
		"const k = Math.max(1, want);",
		"more \\u00b7 drag the map, or press the fit button to see everyone",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js must contain %q", want)
		}
	}
	// The touch rule of the map stays (one finger moves the map: user choice).
	if !strings.Contains(css, "cursor: grab; touch-action: none;") {
		t.Fatal("the map must keep touch-action: none")
	}
	// The 293 phone rules must come after the 292 panel height, or they lose.
	p292 := strings.Index(css, ".map216 > .panel { height: max(380px, calc(100vh - 150px));")
	p293 := strings.Index(css, "/* Bundle 293 (TODO_293)")
	if p292 < 0 || p293 < p292 {
		t.Fatal("the 293 CSS block must sit after the 292 panel rule")
	}
	block := css[p293:]
	for _, want := range []string{
		"@media (max-width: 1100px) {\n  .map216 > .panel { height: auto; }",
		".map292-rows { overflow: visible; margin-right: 0; padding-right: 0; }",
		".map216-legend.one { display: none; }",
		".map216-zoom button:focus:not(:focus-visible) { outline: none; box-shadow: none; }",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("293 CSS block must contain %q", want)
		}
	}
}

func TestAdminWebMapSmoke293(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/map_293_smoke.js", "adminweb/views.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SMOKE_OK") {
		t.Fatalf("map smoke 293 failed: %v\n%s", err, out)
	}
}
