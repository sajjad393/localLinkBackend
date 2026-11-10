# APPLY backend 259 (do this FIRST, then the app 3.0.77)

Zip: `locallink_backend_259_2026-11-11.zip`, on top of GitHub `2b3d9a0`.
Holds: Bundle 259 B1, one new route `GET /api/v1/directory/device/{id}` ("who owns this phone").
No database change. No new setting.

## Commands
Stop the server, then:
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_259_2026-11-11.zip" .
go build ./...
go test ./...
git add -A
git add -f docs/history/TODO_259.md docs/history/APPLY_backend_259.md
git commit -m "v3 backend 259: GET /api/v1/directory/device/{id} returns the owner card of a phone id to his contacts or chat partners only (same 404 for everyone else), so a friend's old phone shows his name instead of Unknown user"
git push
```
Then start the server again.

## What it does
- The app asks it only for chats that have no name (`NAME_FILL_259` in the app log).
- Answer = the same card as `/directory/user/{id}` (number hidden unless public; directory sync off = 404).
- Allowed only when the two accounts are contacts (either way), or when a message exists between any phone of the asker and that phone. Everyone else gets the same 404 as an unknown phone.
- Note: if a phone was given to a NEW owner (web admin "New owner"), a person who had messages with that phone can see the new owner's name. This is rare and only for people who already chatted with that phone.

## Tests
- New: `internal/httpapi/directory_device_259_test.go` (`TestDirectoryByDevice259`).
- Full `go vet` + `go test -p 1 ./...` passed here (EXIT=0). No old test changed.
- An older app never calls this route; an older server answers 404 and the new app just keeps the old name.
