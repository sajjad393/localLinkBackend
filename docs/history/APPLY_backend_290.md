# APPLY backend 290 (web admin crowns, do this FIRST)

Zip: `locallink_backend_290_2026-11-14.zip`, on top of GitHub `6e3427c` (backend 279).
Web files only. No new setting, table or route.

## What changes
- Crown C (lower, round, no pearls) on the web too.
- Each admin level has its own crown + photo ring colour (Owner gold, Operator blue, Security red,
  Auditor silver): Users list photos, Manage panel, Admin level cards, admin sessions, map rows,
  my own identity crown + menu photo.
- Users "Level" pill in the level colour (was gold for all).

## Files
`internal/httpapi/adminweb/components.js`, `views.js`, `app.js`, `styles.css`,
new `internal/httpapi/admin_web_290_test.go`.

## Tests (here)
- `node --check` on the 4 JS files OK. `go vet ./...` OK. `go test ./... -count=1`: 6 packages ok.
- The old web locks (owner hex, `cr217`, `mix217`, `function crown216`, `data-avatar-user`) still pass.

## Commands
Stop the server, then:
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_290_2026-11-14.zip" .
go build ./...
go test ./...
git add internal/httpapi
git add -f docs/history/TODO_290.md docs/history/APPLY_backend_290.md
git commit -m "v3 backend 290: web admin crowns per level (owner gold, operator blue, security red, auditor silver) with matching photo ring and level pill, lower round crown C"
git push
```
Then start the server again (dev command as before). `git add internal/httpapi` (not `-A`) so the
tracked `data/locallink.backup.db` is not uploaded again.
