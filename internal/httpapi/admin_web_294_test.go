package httpapi

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Bundle 294 (TODO_294): phones reached over Wi-Fi Direct / mesh / local
// Wi-Fi stay in colour in the normal rows (only phones nobody reaches go grey
// under OFFLINE); lines keep off photos and crowns; server lines are placed
// first and every line takes the way with the fewest crossings; two lines
// into one phone keep apart.
func TestAdminWebMapReach294(t *testing.T) {
	raw, err := os.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	views := string(raw)
	for _, want := range []string{
		"function reach294(n) {\n    return !!(n && (n.online || (n.link && n.link !== 'offline')));",
		"const online = nodes.filter((n) => reach294(n));",
		"const offline = nodes.filter((n) => !reach294(n));",
		"if (o && o.online === reach294(n))",
		"reach294(n) ? '' : 'off292'",
		"const onY = nodes.filter((n) => reach294(n) && pos[n.device_id])",
		"const offPts = nodes.filter((n) => !reach294(n) && pos[n.device_id])",
		// B: photo top + crown
		"in291.slice().forEach((o) => in291.push({ x: o.x, y: o.y - 40, r: 24 }));",
		"Math.hypot(b.x - o.x, b.y - o.y) * R / (o.r || R)",
		// C: crossings, server lines first, gentle line under the server
		"function cross294(a, b, c, d)",
		"const rank = [clear >= R ? 0 : 1, clear >= R ? 0 : -clear, crosses, bunch, near >= M ? 0 : 1, i];",
		"if (clear >= R && near >= M && !crosses && !bunch) break;",
		"lines294.push({ p, q, pts: best.pts, box: best.box });",
		"(edges[a].from === 'server' ? 0 : 1) - (edges[b].from === 'server' ? 0 : 1) || a - b",
		"const steep294 = e.from === 'server' && Math.abs(q.x - p.x) < Math.abs(q.y - p.y) * 0.6;",
		"steep294 ? [0.15, 0.35, 0.60, 0.85] : [0.60, 0.35, 0.85, 0.15]",
		// D: bunching
		"if (d < Math.PI / 180 * 22) bunch += 1;",
		"const d = d294[i];",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js must contain %q", want)
		}
	}
	for _, gone := range []string{
		"const online = nodes.filter((n) => n.online);",
		"n.online ? '' : 'off292'",
	} {
		if strings.Contains(views, gone) {
			t.Fatalf("views.js still has %q (reached phones would go grey)", gone)
		}
	}
}

func TestAdminWebMapSmoke294(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/map_294_smoke.js", "adminweb/views.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SMOKE_OK") {
		t.Fatalf("map smoke 294 failed: %v\n%s", err, out)
	}
}
