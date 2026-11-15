# APPLY backend 314 - my own chats left out, one open request per chat, nearby group notes

Zip: inside `locallink_314_2026-11-16.zip` (folder `backend`), on top of GitHub `93cd404` (backend 310).
Plan: `docs/history/TODO_314.md`. Push the backend FIRST, then the app 3.0.107+178.

## What changes
- **Own chats (Issue 1).** A Get chat for a chat the signed-in admin's own app user is in is refused:
  409 `own_chat` "You are in this chat. It's already on your phone." A password-only web owner (no app
  user) is not affected. Delete completely / Get file are not affected.
- **One open request per chat (Issue 1b).** A Sent / Reached / Coming request for the same 2 people (any
  order) or the same group blocks a new one, any range: 409 `already_asked` + `open_id`, "This chat already
  has an open request. Open it or cancel it first." Ready, cancelled, empty and closed don't block. Old
  duplicates stay.
- **Lists.** `GET /api/v1/admin/groups`: `me` per group; for the owner also `member_devices` and `nearby`
  (groups reported by members, first page). `GET /api/v1/admin/conversations`: `me` on my own rows.
- **Nearby group notes (Issue 1c).** NEW `POST /api/v1/admin/group-notes` (phone sign-in): signed notes
  `{gid, name, members, made_at, from, at}`, no messages, no keys. Only a member can report; the signature
  is checked with the phone's saved key; the newest note wins; kept 60 days. A reported group can be asked
  like any group (the card goes to the note's member phones; a phone not in the group answers nothing).
- **Web.** The New request picker leaves out me and my groups, lists the nearby groups
  ("Study group · Nearby · 4 members"), no Get chat button on my rows; on `already_asked` it opens that card.
- One small table made on first use (`group_notes_314`, no migration).

## Files (8 + 2 docs)
- NEW `internal/httpapi/admin_chat_314.go`, NEW `internal/httpapi/admin_chat_314_test.go`
- `internal/httpapi/admin_chat_221.go`, `internal/httpapi/admin.go`, `internal/httpapi/admin_conversations.go`
- `cmd/server/main.go` (route)
- `internal/httpapi/adminweb/views.js`, `internal/httpapi/adminweb/app.js`
- `docs/history/TODO_314.md`, `docs/history/APPLY_backend_314.md` (git add -f)

## Tests (run here, Linux)
- `go build ./...` + `go vet ./...` EXIT 0. `gofmt -l` on the 314 files: none.
- `go test ./...`: all packages ok (cmd/server, discovery, httpapi, mdns299, store, tlsauto, ws). EXIT 0.
- New (5): `TestOwnChatIsRefusedOnlyForTheOwnersOwnUser314`, `TestOneOpenRequestPerChatAnyRange314`,
  `TestNearbyGroupNotesSignedMembersOnlyAndAskable314`, `TestConversationsMarkMyChats314`,
  `TestWebHidesOwnChatsAndOpensTheOpenRequest314` - all pass.
- Regression: old tests unchanged and green, incl. `TestGetChatCardGoesToBothPeoplesPhones221` (bad date
  range stays 400) and the web locks `admin_web_221/217/307`.
- **NOT checked here:** real Windows server, real phones, the web page in a browser.

## After the push
1. Restart the server.
2. Web > Conversations > New request: you (if your admin login is linked to your app user) and your
   groups are not in the lists.
3. Ask the same chat twice: the second time the web opens the first card.
