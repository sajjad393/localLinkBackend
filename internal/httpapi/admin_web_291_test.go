package httpapi

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Bundle 291 (TODO_291 B): the web map never puts a phone on top of another,
// keeps lines off third photos and off each other, one fixed order.
func TestAdminWebMapApart291(t *testing.T) {
	raw, err := os.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	views := string(raw)
	for _, want := range []string{
		"function apart291(pos, ids, kept)",
		"apart291(pos, nodes.map((n) => n.device_id), kept291); // Bundle 291 (B-1)",
		"function route291(p, q, C, ks, first, obstacles, mids)",
		"route291(p, q, C, [0.60, 0.35, 0.85, 0.15], first, in291, mids291)",
		"route291(p, q, CP, [0.25, 0.45, 0.10], first, in291, mids291)",
		"nodes.sort((a, b) => rank291(a) - rank291(b)",
		// 217 / 272 lines stay the first choice.
		"arc217(p, q, C, 0.60)", "arc217(p, q, CP, 0.25)",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js must contain %q", want)
		}
	}
	// The spots are saved AFTER the push apart, so the next frame keeps good spots.
	if strings.Index(views, "kept291); // Bundle 291 (B-1)") > strings.Index(views, "mapView.pos[n.device_id] = { x: q.x, y: q.y, online: !!n.online }") {
		t.Fatal("push apart must run before the spots are kept")
	}
}

func TestAdminWebMapSmoke291(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the smoke runs in CI")
	}
	out, err := exec.Command(node, "testdata/map_291_smoke.js", "adminweb/views.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SMOKE_OK") {
		t.Fatalf("map smoke failed: %v\n%s", err, out)
	}
}
