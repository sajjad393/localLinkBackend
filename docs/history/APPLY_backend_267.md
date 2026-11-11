# APPLY backend 267 (do this FIRST, then the app 3.0.83)

Zip: `locallink_backend_267_2026-11-14.zip`, on top of GitHub `ca4f41c` (backend 259).
Holds: Bundle 267 A, one new route `GET /api/v1/directory/people` ("people I can add").
No database change. No new setting.
Also adds `.github/workflows/backend_ci.yml` (Backend CI, see below).

## Commands
Stop the server, then:
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_267_2026-11-14.zip" .
go build ./...
go test ./...
git add -A
git add -f docs/history/TODO_267.md docs/history/APPLY_backend_267.md
git add -f .github/workflows/backend_ci.yml
git commit -m "v3 backend 267: GET /api/v1/directory/people lists everyone the requester can add (active, findable by name, directory sync on, not the requester, not already a contact), 50 a page in name order with more=true when another page exists, phone hidden unless public - the Add Contact screen shows it while the search box is empty"
git push
```
Then start the server again.

## What it does
- The app calls it when Add Contact opens with an empty search box (`ADD_LIST_267` in the app log).
- Same people and same card as the name search: active users, findable by name, directory sync on. Phone number hidden unless public.
- Leaves out you and the people already in your contacts.
- 50 a page in name order; `more=true` when another page exists.
- Same privacy as today: anyone could already list these people by searching one letter.

## Tests
- New: `internal/httpapi/directory_people_267_test.go` (contacts, self, hidden people, paging, 401 without login).
- `go vet` + `go test -p 1 ./...` passed here (EXIT=0). No old test changed.
- Older apps never call this route. On an older server the new app gets 404 and shows the people it knows from nearby phones.
- Bundle 266 (internet call voice) needs NO backend change: the 251 voice relay is already on the server.
- Here (14 Nov audit): `go build` OK, `go vet` OK, `go test ./...` OK (all 6 packages), `go test -race ./...` OK (no data race). `gofmt` lists 5 old files (not changed by 267).

## Backend CI (new)
- `.github/workflows/backend_ci.yml` runs on every push to `main` (and by hand from the Actions tab).
- It runs gofmt (warning only), `go build`, `go vet`, `go test`, `go test -race` and keeps the logs as artifact `go-results`.
- It never blocks a push. A red mark only means look at the log. No secrets, no server start.
