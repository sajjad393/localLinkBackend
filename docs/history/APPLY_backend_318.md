# APPLY backend 318 - "Get new messages only" facts pass through (with app 3.0.109+180)

Zip: inside `locallink_318_2026-11-16.zip` (`locallink_backend_318_2026-11-16.zip`), on top of GitHub `421d477`
(backend 317). Plan: `docs/history/TODO_318.md`. Push the backend FIRST, then the app.

## What changes
- A Get chat request from the Owner phone may carry `since`, `since_n`, `since_h` (new messages only). The server
  checks `since` (bad = dropped) and puts them in the card detail for the phones of the chat.
- The answer read returns `since`, `days`, `up_to_date` only for new-only answers. Full answers are unchanged.
- The web owner is unchanged: always the full chat (no chat storage on the server).
- No new table, no new route.

## Files (2 + 2 docs)
- `internal/httpapi/admin_chat_221.go`, NEW `internal/httpapi/admin_chat_318_test.go`
- `docs/history/TODO_318.md`, `docs/history/APPLY_backend_318.md` (git add -f)

## Tests (Linux)
- `go test ./...` all ok (cmd/server, discovery, httpapi, mdns299, store, tlsauto, ws). `go vet ./internal/httpapi/` OK. gofmt clean on both files.
- New: `TestNewOnlyRequestPassesSince318`, `TestFullRequestHasNoSinceAndBadSinceIsDropped318`,
  `TestNewOnlyAnswerFactsReachTheOwner318`, `TestFullAnswerHasNoNewOnlyFacts318` pass.
- NOT checked: Windows server.

## After the push
Restart the server.
