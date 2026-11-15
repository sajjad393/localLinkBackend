# APPLY backend 317 - refused files shown, web viewer states (with app 3.0.108+179)

Zip: inside `locallink_317_2026-11-16.zip` (folder `backend`), on top of GitHub `6db45b6` (backend 314).
Plan: `docs/history/TODO_317.md`. Push the backend FIRST, then the app.

## What changes
- Request rows get `refused` (e.g. `file_gone`) from `admin_action_receipts.result`.
- Web viewer: group event lines as a grey centre line; deleted = faded + red "Deleted 11:41" (date only on
  another day); no Get button - Next in line / Coming x / y / shows / Not on the phone any more / Too big;
  live card files strip.
- No new table, no new route.

## Files (7 + 2 docs)
- `internal/httpapi/admin_chat_221.go`, NEW `admin_chat_317.go`, NEW `admin_chat_317_test.go`, NEW `admin_web_317_test.go`
- `internal/httpapi/adminweb/views.js`, `adminweb/styles.css`, NEW `internal/httpapi/testdata/conv_317_smoke.js`
- `docs/history/TODO_317.md`, `docs/history/APPLY_backend_317.md` (git add -f)

## Tests (Linux)
- `go test ./...` all ok (cmd/server, discovery, httpapi, mdns299, store, tlsauto, ws). `go vet ./...` EXIT 0. gofmt clean on 317 files.
- New: `TestRefusedFileShowsWhy317`, `TestAdminWebViewer317`, `TestAdminWebSmoke317` pass; old web locks green.
- NOT checked: Windows server, the web page in a real browser.

## After the push
Restart the server. Web > Conversations > open a chat: no Get buttons, file states in words.
