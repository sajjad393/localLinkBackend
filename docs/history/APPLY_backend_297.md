# APPLY backend 297 (web Map: ring fix + Full screen)

Zip: `locallink_backend_297_2026-11-14.zip`, on top of GitHub `775c3d8` (backend 296).
Web admin only. App unchanged (stays 3.0.94+165). No new table, setting or route.

## What changes
- A: the Phones list no longer cuts the left side of the coloured role ring.
- B: a Full screen button under the zoom buttons (4 corners icon). The Fit button
  gets a target icon so the two do not look the same.
  - Click: the browser goes full screen; only the map, the zoom buttons and
    "Exit full screen" (top right) stay. Sidebar, top bar, chips, legend and
    Phones panel are hidden.
  - Exit: the button, Esc, the browser's own exit, or opening another page.
  - No full screen support (iPhone Safari): the map fills the page instead.
  - The map fits everyone on enter and exit. Live updates, zoom, drag and pinch
    keep working; full screen stays on through live updates.
- Not changed: map layout (292-294), live data (296), lines, colours, other pages.

## Files
`internal/httpapi/adminweb/index.html`, `views.js`, `styles.css`, `app.js`;
new `internal/httpapi/admin_web_297_test.go`;
`docs/history/TODO_297.md`, `docs/history/APPLY_backend_297.md`. Old tests unchanged.

## Tests (here)
- `go test -count=1 ./...` 6 packages ok (exit 0); `go vet ./internal/httpapi` OK; JS syntax OK.
- Real build in Chromium (PC 1341x597, 1920x960, phone 360x690), 0 page errors:
  ring fully inside the list; full screen = whole window; stays on after a live
  redraw; Esc, Exit and page change all end it.
- Not tested in your real browser / iPhone yet.

## Commands
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_297_2026-11-14.zip" .
go build ./...
go test ./...
git add internal/httpapi
git add -f docs/history/TODO_297.md docs/history/APPLY_backend_297.md
git commit -m "v3 backend 297: web map Phones list shows the full role ring, Full screen button (only map + zoom + Exit, Esc exits, page-fill fallback, survives live updates), Fit gets a target icon"
git push
```
Restart the server, then hard refresh the admin page (Ctrl+F5).
