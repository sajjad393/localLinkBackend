package httpapi

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Bundle 292 (TODO_292): the web map fills the page, opens at a zoom where
// names are readable (100 - 140 %), offline phones in a grey row under an
// OFFLINE line, the legend in the top bar, a "+N more" pill.
func TestAdminWebMapFill292(t *testing.T) {
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
		"function layout216(nodes, edges, stageW)",
		"function fit292(points, stageW, stageH)",
		"const k = Math.max(1, want);", // never below 100 % by itself
		"Math.min(1.4,",
		"function outside292(points, view, stageW, stageH)",
		"more \\u00b7 drag the map, or press the fit button to see everyone",
		"<span>OFFLINE</span>",
		"reach294(n) ? '' : 'off292'", // TODO 294: reached phones are not grey
		"${ctx.liveBadge ? ctx.liveBadge() : ''}<div class=\"map216-legend\">${legend}</div></div>",
		"data-map-pct",
		"mapView.fitAll = true; fit();",
		"autoFit();",
		// 217 / 272 / 291 locks stay.
		"fit217(", "arc217(p, q, C, 0.60)", "arc217(p, q, CP, 0.25)", `data-map-zoom="fit"`, "Fit everyone",
		"apart291(pos, nodes.map((n) => n.device_id), kept291); // Bundle 291 (B-1)",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js must contain %q", want)
		}
	}
	for _, gone := range []string{"<svg width=\"1000\" height=\"700\"", "const W = 1000; const H = 700;\n    const pos = { server: { x: W / 2, y: 110, fixed: true } };"} {
		if strings.Contains(views, gone) {
			t.Fatalf("views.js still has %q", gone)
		}
	}
	at := strings.Index(css, ".map216-stage {")
	if at < 0 {
		t.Fatal("no .map216-stage rule")
	}
	stage := css[at : at+strings.Index(css[at:], "}")]
	if !strings.Contains(stage, "height: max(380px, calc(100vh - 150px))") || strings.Contains(stage, "clamp(320px, 52vh, 620px)") {
		t.Fatal("the map must fill the page (max(380px, 100vh - 150px))")
	}
	for _, want := range []string{
		".map216-node .tag { font: 600 12px",
		".map216 > .panel { height: max(380px, calc(100vh - 150px));",
		".map292-rows {", ".map216-node.off292 .av216 { filter: grayscale(1); }", ".map292-more {", ".map292-pct {",
		".map216-legend { margin-left: auto;",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css must contain %q", want)
		}
	}
	if strings.Contains(css, ".map216-legend { position: absolute;") {
		t.Fatal("the legend must not sit inside the map any more")
	}
}

func TestAdminWebMapSmoke292(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/map_292_smoke.js", "adminweb/views.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SMOKE_OK") {
		t.Fatalf("map smoke 292 failed: %v\n%s", err, out)
	}
}
