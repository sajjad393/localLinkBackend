package httpapi

import (
	"os"
	"strings"
	"testing"
)

// Bundle 297 (TODO_297): A the Phones list no longer cuts the left side of the
// role ring; B a Full screen button shows only the map, with an Exit button
// (and Esc / the browser's own exit), kept across live redraws.
func read297(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("adminweb/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAdminWebPhonesRing297(t *testing.T) {
	css := read297(t, "styles.css")
	if !strings.Contains(css, ".map292-rows { padding-left: 4px; margin-left: -4px; }") {
		t.Fatal("the Phones list needs room on the left for the 2.6 px ring")
	}
	// The ring itself is unchanged (2.6 px box-shadow outside the photo).
	if !strings.Contains(css, ".av216.is-admin { box-shadow: 0 0 0 1px var(--background), 0 0 0 2.6px #e7b416; }") {
		t.Fatal("owner ring changed")
	}
}

func TestAdminWebMapFullScreen297(t *testing.T) {
	views := read297(t, "views.js")
	for _, want := range []string{
		`<button data-map-full297 aria-label="Full screen" title="Full screen">${ui.icon('i-full297')}</button>`,
		`<button type="button" class="map297-exit" data-map-exit297 aria-label="Exit full screen">${ui.icon('i-close')}<span>Exit full screen</span></button>`,
		`<div class="map216-stage${mapView.full297 ? ' full297' : ''}" data-map-stage>`,
		"if (mapView.full297) return; // Bundle 297: the full screen map is sized by CSS",
		"root.requestFullscreen().catch(() => {})",
		"root.webkitRequestFullscreen()",
		"d.exitFullscreen().catch(() => {})",
		"if (ev.key === 'Escape' && mapView.full297 && mapView.setFull297) mapView.setFull297(false);",
		"document.addEventListener('fullscreenchange', left);",
		"document.addEventListener('webkitfullscreenchange', left);",
		"setTimeout(() => { if (stage.isConnected) { fitHeight293(); autoFit(); } }, 120);",
		"leaveFull297() {",
		"if (ev.target.closest('[data-map-pick],[data-map-zoom],[data-map-full297],[data-map-exit297]')) return;",
		// Fit keeps its words (old locks) next to the new button.
		`<button data-map-zoom="fit" aria-label="Fit everyone" title="Fit everyone">${ui.icon('i-fit')}</button>`,
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js must contain %q", want)
		}
	}
	css := read297(t, "styles.css")
	for _, want := range []string{
		".map216-stage.full297 { position: fixed; inset: 0; z-index: 1000;",
		".full297 .map297-exit { display: inline-flex; }",
		".map297-exit { display: none;",
		".full297 .map216-zoom { top: 58px; }",
		".full297 [data-map-full297] { display: none; }",
		"body.full297-on { overflow: hidden; }",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css must contain %q", want)
		}
	}
	shell := read297(t, "index.html")
	if !strings.Contains(shell, `<symbol id="i-full297" viewBox="0 0 24 24"><path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5"/></symbol>`) {
		t.Fatal("Full screen gets the 4 corners icon")
	}
	if !strings.Contains(shell, `<symbol id="i-fit" viewBox="0 0 24 24"><circle cx="12" cy="12" r="7"/>`) {
		t.Fatal("Fit gets the target icon (must not look like Full screen)")
	}
	app := read297(t, "app.js")
	if !strings.Contains(app, "if (viewId !== 'map' && LL.views.map && LL.views.map.leaveFull297) LL.views.map.leaveFull297();") {
		t.Fatal("another page must end the map full screen")
	}
}
