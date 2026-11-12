# APPLY backend 272-276 (do this FIRST, then the app 3.0.85)

Zip: `locallink_backend_272-276_2026-11-14.zip`, on top of GitHub `86aa525` (backend 267).
No new setting. No new table. One new route: `GET /api/v1/admin/live` (plain ws, keep http).
At start the server fixes old profile names once (`NAME_FIX_276 n=..` in the log, safe to repeat).

## Commands
Stop the server, then:
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_272-276_2026-11-14.zip" .
go build ./...
go test ./...
git add -A
git add -f docs/history/TODO_272.md docs/history/TODO_274.md docs/history/TODO_276.md docs/history/APPLY_backend_272-276.md
git commit -m "v3 backend 272-276: admin live map socket GET /api/v1/admin/live (first-frame auth, link_map frames at most 1 a second, online = connected now, admin_link_map_changed hint to admin phones), web panel live map/dashboard/network with 15 s fallback poll, dashboard colours fixed (each phone once), blue server lines, smaller bow, Network page real mesh/Wi-Fi links, never show My Android Phone names (username without @, <name>'s phone)"
git push
```
Then start the server again.

## What changes
- 274: web Map, Dashboard "Live network" and Network page update by themselves (green "Live" dot). If the socket drops: "Reconnecting..." and a 15 s poll.
- 274 D: dashboard colours are right again (was all blue); the donut counts each phone once; the row counts links; "Mesh capable" is gone.
- 272: server lines blue; phone-to-phone lines bow less; dragging the map does not select text; Network tiles = real links, "Link now" shows Server + Wi-Fi, Local Wi-Fi tile.
- 276: no "My Android Phone (xxxx)" anywhere on the web or in names the server sends to phones. Person = display name, else username (no @). Phone = its typed name, else "<name>'s phone".

## Tests (here)
- `go build ./...` OK, `go vet ./...` OK, `go test ./...` OK (6 packages, 328 tests), `go test -race` on the 216/274/276/web tests OK.
- New: `name_276_test.go`, `admin_live_274_test.go`, `admin_web_272_test.go`, `admin_web_274_test.go`, `admin_web_276_test.go`.
- Old-test changes (listed in TODO_276): `profile_test.go` and `admin_216_fields_test.go` expected the phone name as the person's name (the bug).
- `gofmt` lists 5 old files (not changed here).
