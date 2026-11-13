# APPLY backend 293 (web Map on phone browsers)

Zip: `locallink_backend_293_2026-11-14.zip`, on top of GitHub `1c4fedc` (backend 291+292).
Web admin only. No new setting, table or route. The app does not change. Computer view unchanged.

## What changes (mock `mocks/web_map_293_phone.html`)
- Phones / tablets: the map ends 64 px above the bottom of the visible screen, so everybody
  (offline row too) is on screen and the top of the Phones list peeks under it. Scroll from there,
  the top bar or the chips. One finger on the map still moves the map.
- The Phones list under the map takes its own height again (no empty space).
- 2 phones in a row: no half-step shift; server lines stay inside the map.
- Phone browsers start a little zoomed out: 90 %, down to 80 % with many phones (was 100 - 140 %).
- Short pill on phones: "+N more · drag the map to see them". One-item legend hidden on phones.
  No ring left on the zoom buttons after a tap.

## Files
`internal/httpapi/adminweb/views.js`, `internal/httpapi/adminweb/styles.css`,
new `internal/httpapi/admin_web_293_test.go`, new `internal/httpapi/testdata/map_293_smoke.js`,
`docs/history/TODO_293.md`, `docs/history/APPLY_backend_293.md`.

## Tests (here)
- `node --check` OK; smoke 291 / 292 / 293 OK; `go vet ./...` OK; `go test -count=1 ./...` 6 packages ok (exit 0).
- Real admin page in Chromium (faked API) at 360x690, 320x560, 412x800, 800x1000, 1352x607, 1920x960:
  no script errors; phones: 90 % / 80 %, everybody visible with 4 phones; computer: same zoom and same line paths as `1c4fedc`.
- Not tested on a real phone browser yet.

## Commands
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_293_2026-11-14.zip" .
go build ./...
go test ./...
git add internal/httpapi
git add -f docs/history/TODO_293.md docs/history/APPLY_backend_293.md
git commit -m "v3 backend 293: web Map on phones fits the screen with the Phones list peeking under it, 80-90% default zoom on phones, 2 in a row without stagger, lines stay inside the map, short +N more text"
git push
```
Restart the server, then reload the admin page on the phone.
