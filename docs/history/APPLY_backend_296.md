# APPLY backend 296 (H4: admin map shows only live links)

Zip: `locallink_backend_296_2026-11-14.zip`, on top of GitHub `9be3909` (backend 294).
Deploy this BEFORE app 3.0.94+165. Works with the old app too (nothing breaks).

## What changes
- A phone's link report counts 45 s (was 4 min).
- A report counts only while that phone is on the server now (a phone that left
  cannot keep drawing its old lines).
- New WebSocket frame `link_report_296` (`{"type":"link_report_296","neighbors":[...]}`):
  the new app sends only proven links, every 15 s and at once on a change. An empty
  list removes the lines at once. Rate limit 30/min per phone, 120/min per account.
  No presence relay to other phones for this frame.
- While an admin watches, the map is checked every 5 s and pushed when it changed
  (and every 30 s anyway). Before: a report running out pushed nothing for up to 30 s.
- The 294 layout does not change. No new table, setting or route.

## Files
`internal/httpapi/admin_216.go`, `admin_live_274.go`, `websocket.go`;
new `internal/httpapi/admin_live_296.go`, `admin_live_296_test.go`;
`docs/history/TODO_296.md`, `docs/history/APPLY_backend_296.md`.
Old tests unchanged (`TestLinkMapDropsOldReports216` still passes; its message still says 4 minutes).

## Tests (here)
- `go build ./...` exit 0; `go vet ./internal/httpapi` OK; `go test -count=1 ./...` 6 packages ok (exit 0).
- New: 45 s life, online-only, empty list clears, map signature, 5 s sweep push, wiring (6 tests).
- Not tested with your real server and phones yet.

## Commands
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_296_2026-11-14.zip" .
go build ./...
go test ./...
git add internal/httpapi
git add -f docs/history/TODO_296.md docs/history/APPLY_backend_296.md
git commit -m "v3 backend 296: admin map shows only live links (reports count 45 s, only from phones online now, new link_report_296 frame, 5 s check pushes changes at once)"
git push
```
Restart the server.
