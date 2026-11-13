# APPLY backend 292 (web Map fills the page, readable default zoom)

Zip: `locallink_backend_292_2026-11-14.zip`, on top of GitHub `69f43c8` (backend 290).
**The zip also carries backend 291** (409 text + map keep-apart), because 291 is not on GitHub yet.
Web only (+ the 291 409 text). No new setting, table or route. The app does not change.

## What changes (mock `mocks/web_map_292.html`, approved)
- The map fills the page: height `max(380px, 100vh - 150px)` (457 px on a 607 px window, was 320 px).
  The Phones panel is as high; its list scrolls inside.
- Default zoom 100 % - 140 %, never below 100 % by itself: names 12 px (were 5.5 px at zoom 0.5).
  "+", "-", wheel, pinch and the fit button work as before (fit button = everybody, can go below 100 %).
  Small zoom text under the buttons ("100%").
- Layout: server top centre, online phones in rows under it (130 px apart, a new row when the map is full),
  offline phones grey in a row under an "OFFLINE" line. 291 keep-apart + line rules unchanged.
- Too many to fit at 100 %: a pill at the bottom "+N more · drag the map, or press the fit button to see everyone".
- Legend moved from inside the map to the top bar (right side).

## Files
292: `internal/httpapi/adminweb/views.js`, `internal/httpapi/adminweb/styles.css`,
new `internal/httpapi/admin_web_292_test.go`, `internal/httpapi/testdata/map_292_smoke.js`,
changed `internal/httpapi/testdata/map_291_smoke.js` ("inside" check uses the layout bounds).
291 (if not pushed): `admin_chat_221.go`, `adminweb/views.js`, `lock_key_291_test.go`, `admin_web_291_test.go`,
`testdata/map_291_smoke.js`.

## Tests (here)
- `node --check` on all 4 JS files OK. `go vet ./...` OK. `go test -count=1 ./...`: 6 packages ok (exit 0).
- New smoke (real views.js code in node), owner's screen 794 x 457: 4 phones zoom 1.03, all visible, no pill;
  12 phones zoom 1.00, "+4 more"; 30 phones zoom 1.00, "+20 more"; min gap >= 129 px; offline row under all online.
  FAILS with the 100 % floor removed (zoom 0.79 / 0.41) and with the old views.js.
- Real admin page in Chromium (real index.html / app.js / styles.css, faked API): 1352 x 607, 1920 x 960 and
  390 x 800 phone, dark + light: no script errors; fit / zoom / filter / drag update the % text and the pill.
- Old locks (216 / 217 / 272 / 274 / 290 / 291) pass.

## Commands
Stop the server, then:
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_292_2026-11-14.zip" .
go build ./...
go test ./...
git add internal/httpapi
git add -f docs/history/TODO_291.md docs/history/APPLY_backend_291.md docs/history/TODO_292.md docs/history/APPLY_backend_292.md
git commit -m "v3 backend 291+292: clear 409 text when phones have not sent their lock key yet, web Map keeps phones and lines apart, map fills the page with readable 100-140% default zoom, offline phones grey under an OFFLINE line, legend in the top bar, +N more pill"
git push
```
Then start the server again and hard-refresh the admin page (Ctrl+F5). `git add internal/httpapi` (not `-A`)
so the tracked `data/locallink.backup.db` is not uploaded again.
