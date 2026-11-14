package httpapi

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Bundle 307 (TODO_307 Part D): the web Conversations page in bold cards -
// top bar, "Live requests" row of Uber-style cards, two columns (400 px list
// + viewer), Chats / Requests switch, In progress + Finished, viewer header
// with Ready / Download / Delete copy, no route words, quiet animated refresh.
func TestAdminWebConversations307(t *testing.T) {
	raw, err := os.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	views := string(raw)
	rawCSS, err := os.ReadFile("adminweb/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(rawCSS)
	for _, want := range []string{
		// D1
		"Chats the phones keep \\u00b7 Owner only", "Server connected", "data-conv307-theme", "${PLUS307}New request",
		// D2
		"<b>Live requests</b>", "data-conv307-viewall", "live.slice(0, 3).map(liveCard307)", "class=\"hc307 s-${st.k}${pop}\"",
		// D3 + D4
		"<div class=\"wcols307\">${listCard307()}${right}</div>", "data-conv307-tab=\"chats\"", "data-conv307-tab=\"requests\"",
		"Search people or groups", "<span>In progress</span>", "<span>Finished</span>", "data-conv307-cancel",
		"q.id === scope.viewId ? ' sel' : ''",
		// D5
		"data-conv307-download", "Delete copy", "Ready</span>` : ''",
		// D6
		"const route = '';",
		// D8
		"function animate307()", "card.style.setProperty('--p', to)", "scope.prev307 = next;", "prefers-reduced-motion: reduce",
		// the list keeps the old click hooks
		"data-conv221-open=\"${ui.esc(q.id)}\"", "data-conv221-ask=\"${i}\"", "data-conv221-new",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js must contain %q", want)
		}
	}
	// D6: the constant stays, the page no longer prints it.
	if !strings.Contains(views, "const ROUTE221 = {") || strings.Contains(views, "ROUTE221[m.route] ? `<span class=\"tag221\">") {
		t.Fatal("ROUTE221 must stay but must not be printed on the bubbles")
	}
	for _, want := range []string{
		".wcols307 { display: grid; grid-template-columns: 400px minmax(0, 1fr);",
		".wlg307 { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr));",
		".hc307.s-ready { --c: var(--k-ok); box-shadow: var(--k-shadow), 0 0 0 2px var(--k-ok); }",
		"@property --p", ".car307 {", ".ring307 {", "@keyframes pop307", "@keyframes shim307",
		"[data-theme=\"dark\"] .conv221 { --k-card: #161b22;",
		".c307 *, .c307 *::before, .c307 *::after { animation: none !important; transition: none !important; }",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css must contain %q", want)
		}
	}
}

func TestAdminWebConversationsSmoke307(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/conv_307_smoke.js", "adminweb/views.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SMOKE_OK") {
		t.Fatalf("conversations smoke 307 failed: %v\n%s", err, out)
	}
}
