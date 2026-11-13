# APPLY backend 294 (web Map: reached phones in colour, lines off crowns, fewest crossings)

Zip: `locallink_backend_294_2026-11-14.zip`, on top of GitHub `da74731` (backend 293).
Web admin only. No new setting, table or route. The app does not change.

## What changes (mock `mocks/web_map_294.html`)
- A phone reached over Wi-Fi Direct, mesh or local Wi-Fi is in the normal rows, in colour.
  Only phones nobody reaches go grey under the OFFLINE line.
- Lines keep off other phones' photos and crowns.
- A phone under the server gets a gentle line (no big loop). Server lines are placed first; every line
  takes the way with the fewest crossings. Lines that must cross still cross, as few times as possible.
- Two lines into the same phone keep at least 22 degrees apart when a way exists.

## Files
`internal/httpapi/adminweb/views.js`; new `internal/httpapi/admin_web_294_test.go`,
new `internal/httpapi/testdata/map_294_smoke.js`; listed old test changes:
`admin_web_291_test.go` (route291 strings, `online: reach294(n)`), `admin_web_292_test.go`
(`reach294(n) ? '' : 'off292'`), `testdata/map_291/292/293_smoke.js` (`'reach294'` added to the grabbed
functions); `docs/history/TODO_294.md`, `docs/history/APPLY_backend_294.md`.

## Tests (here)
- `node --check` OK; smoke 291-294 OK; `go vet ./...` OK; `go test -count=1 ./...` 6 packages ok (exit 0).
- Real admin page in Chromium (faked API) at 1341x597, 360x690, 1920x960 with your screenshot's people:
  no script errors, sumaira in colour, 0 crossings, no line over a crown.
- Mutation check: 7 cuts, each caught. Not tested with your real server yet.

## Commands
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_294_2026-11-14.zip" .
go build ./...
go test ./...
git add internal/httpapi
git add -f docs/history/TODO_294.md docs/history/APPLY_backend_294.md
git commit -m "v3 backend 294: web Map shows phones reached over Wi-Fi Direct or mesh in colour (not offline), lines keep off photos and crowns, fewest line crossings, gentle line under the server, lines into one phone keep apart"
git push
```
Restart the server, then reload the admin page (Ctrl+F5).
