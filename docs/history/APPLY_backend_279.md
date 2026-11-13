# APPLY backend 279 (do this FIRST, then the app 3.0.87 / 3.0.88 / 3.0.89 zips)

Zip: `locallink_backend_279_2026-11-14.zip`, on top of GitHub `106c750` (backend 272-276).
No new setting, no new table, no new route.

## What changes
- Web Users no longer shows a person twice ("Active" + "Not on server yet").
  - B-1: a phone's "seen offline" report for an offline id that is already linked to a live user
    (`offline_ids`, written when the account went on the server) is dropped, and its old row removed.
  - B-2: the "Not on server yet" list leaves out rows whose phone has joined, or whose phone's offline
    id is linked to a live user. Old rows (like kiran / sumaira) go away at once, no wait.
- Rows are not deleted by B-2 (read-time filter only). A real not-yet-joined account still shows.

## Files
`internal/httpapi/join_relay_222.go` (`noteOfflineSeen222`, `notOnServer222`),
new `internal/httpapi/users_dup_279_test.go` (2 tests).

## Tests (here)
- `go vet ./internal/httpapi/` OK. `go test ./... -count=1` -> GO_EXIT=0, 6 packages ok, 330 tests (328 + 2 new).
- The 2 new tests FAIL on the old `join_relay_222.go` and pass on the new one.

## Commands
Stop the server, then:
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_279_2026-11-14.zip" .
go build ./...
go test ./...
git add -A
git add -f docs/history/TODO_279.md docs/history/APPLY_backend_279.md
git commit -m "v3 backend 279: web Users shows each person once - drop offline-seen reports for offline ids already linked to a live user, Not on server yet list leaves out joined phones and linked offline ids (old rows vanish at once)"
git push
```
Then start the server again (dev command as before).
