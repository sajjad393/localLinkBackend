# APPLY backend 307 - Get chat requests close by themselves + new web Conversations

Zip: `locallink_backend_307_2026-11-15.zip`, on top of GitHub `26df549` (backend 300).
Plan: `docs/history/TODO_307.md` (Parts A + D). Push the backend FIRST, then the app 307 (3.0.101+172).

## What changes
- **A1 - "No copy here" notes reach the server.** Phones upload their signed "seen" notes in the normal admin-actions sync
  (a relay phone uploads the notes it carried too). Each note is checked with the phone's own signing key.
- **A2 - a chat nobody has closes as "No messages".** When every target phone said "empty", the request becomes
  `cancelled` + `empty: true`, and the request list returns `empty`. Notes that came before the label close it when the label comes.
- **A4 - offline requests show on the web with names.** The chat (people / group) comes with the label, so the web can list
  it and fetch its parts. Each target phone is named after its person.
- **A5 - final state of an offline request.** The admin phone that made it uploads done / cancelled / empty once.
- **Live numbers for the web card:** `phones`, `reached` ("1 of 2 phones"), `from_name` ("Answer coming from ayesha's phone"),
  `opened`, `closed`.
- **Web Conversations (D1-D8), English only:** top bar (server pill, dark mode, "+ New request"), "Live requests" row (up to 3
  cards; Ready has a green edge), two columns (list 400 px + viewer), Chats / Requests switch, "In progress" + "Finished", the
  viewer with date chips + blue bubbles + "Deleted by" tag. No path words (Server / Wi-Fi Direct / Mesh). The quiet refresh only
  animates what changed. Reduce-motion respected.
- Two small tables made on first use (no migration).

## Files (8 + 2 docs)
- `internal/httpapi/admin_actions_220.go` (sync: seen notes + closed states; label: chat + person per phone)
- `internal/httpapi/admin_chat_221.go` (list: `empty`, `phones`, `reached`, `from_name`, `opened`, `closed`)
- new `internal/httpapi/admin_chat_307.go`
- new `internal/httpapi/admin_chat_307_test.go`, `internal/httpapi/admin_web_307_test.go`, `internal/httpapi/testdata/conv_307_smoke.js`
- `internal/httpapi/adminweb/views.js`, `internal/httpapi/adminweb/styles.css`
- `docs/history/TODO_307.md`, `docs/history/APPLY_backend_307.md` (git add -f)

## Tests (run here, Linux)
- `go vet ./...` OK. gofmt: the changed/new files are clean.
- Full `go test ./...`: all packages ok (cmd/server, discovery, httpapi, mdns299, store, tlsauto, ws). EXIT 0.
- New: `TestSeenNotesCloseAsNoMessages307` (bad signature refused, all empty -> cancelled + empty),
  `TestOfflineRequestChatLabelAndFinalState307`, `TestSeenNotesBeforeLabelCloseOnLabel307`,
  `TestAdminWebConversations307` (+ every 221 lock kept), `TestAdminWebConversationsSmoke307` (page script runs).
- Old locks unchanged and green: `admin_web_221_test.go`, `admin_web_217_test.go` (reduce-motion block), `admin_overview_test.go`.
- **NOT checked here:** real Windows server, real browser look (light + dark), real phones.

## After the push
1. Restart the server.
2. PC Chrome: Admin -> Conversations. Check the live row, the list, the viewer, light + dark.
3. Then install the app 307 and do the phone checks in `APPLY_app_307.md`.
