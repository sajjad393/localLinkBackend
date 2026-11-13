# APPLY backend 291 (409 text + web Map keeps phones and lines apart, deploy FIRST)

Zip: `locallink_backend_291_2026-11-14.zip`, on top of GitHub `6e3427c` (backend 279).
**The zip also carries the backend 290 files** (web crowns), because 290 is not on GitHub yet.
If you already pushed 290, the 290 files in the zip are the same and nothing changes for them.
Web + one text only. No new setting, table or route.

## What changes
- A (409 text): web "Request from phones" with no lock key on any phone of the chat now says
  "These phones have not sent their lock key yet. They send it by themselves when they open the new
  LinkUp and reach the server. Try again after that." (was "no phone of this chat can be reached (no keys yet)").
  The real fix is in the app 3.0.92 (it sends the key by itself).
- B-1 / B-2 (web Map): a new phone never lands on a phone already on the map. Fixed order:
  server phones first, then by name. Gap between phones = 120 px up to about 13 phones; with more
  phones it shrinks to fit the map (never below 64 px). Phones you already see do not move.
- B-3 (web Map lines): a line that would run under a third phone bows the other way or less;
  two lines on top of each other: the second one moves. Free lines keep today's bow (0.60).

## Files
290 (if not pushed): `internal/httpapi/adminweb/components.js`, `views.js`, `app.js`, `styles.css`,
`internal/httpapi/admin_web_290_test.go`.
291: `internal/httpapi/admin_chat_221.go`, `internal/httpapi/adminweb/views.js`,
new `internal/httpapi/lock_key_291_test.go`, `internal/httpapi/admin_web_291_test.go`,
`internal/httpapi/testdata/map_291_smoke.js`.

## Tests (here)
- `go vet ./...` OK. `go test ./...`: 6 packages ok (exit 0).
- New `lock_key_291_test.go` FAILS without the new text (checked by stashing the fix).
- New `admin_web_291_test.go`: string locks + a node smoke run of the real map code with
  3 / 6 / 12 / 30 / 60 phones: smallest gap 298.9 / 119.6 / 119.5 / 79.0 / 63.5 px, all inside the map;
  blocked lines move, free lines keep 0.60, kept phones stay. (The smoke run skips when node is not
  installed, like on GitHub CI.)
- Old web locks (arc 0.25 / 0.60, crowns 216/217/290) still pass.

## Commands
Stop the server, then:
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_291_2026-11-14.zip" .
go build ./...
go test ./...
git add internal/httpapi
git add -f docs/history/TODO_290.md docs/history/APPLY_backend_290.md docs/history/TODO_291.md docs/history/APPLY_backend_291.md
git commit -m "v3 backend 290+291: web admin crowns per level, clear 409 text when phones have not sent their lock key yet, web Map keeps phones apart (server phones first, then by name) and moves lines off other phones"
git push
```
Then start the server again (dev command as before). `git add internal/httpapi` (not `-A`) so the
tracked `data/locallink.backup.db` is not uploaded again.
