# APPLY backend 310 - web chat like the app + why a request is stuck at "Sent"

Zip: inside `locallink_310_2026-11-15.zip` (folder `backend`), on top of GitHub `25921b6` (backend 307).
Plan: `docs/history/TODO_310.md`. Push the backend FIRST, then the app 310 (3.0.103+174).

## What changes
- **V2 - voice notes play on the web.** `audio/*` and `voice_*.m4a` (also aac / ogg / opus / mp3 / wav) show as a voice
  bubble: round button, bars like the app, length. States: not on the PC yet (download arrow) -> getting (ring) ->
  ready (play) -> playing (pause, played part blue). Phones on 3.0.102 or older send no bars: flat bars + size.
- **V3 - bubbles like the app's Messages screen.** Time at the bottom inside the bubble; names only in groups, on the first
  bubble of a run; the 3-dot menu only on hover / keyboard focus; the app's grey bubbles (light + dark); short file names
  ("LinkUp_monthly…_final_v3.pdf").
- **P2 - a phone that connects gets a nudge.** When a target phone of an open request connects to the server, the
  server tells it to check its requests (at most once per 30 s per phone).
- **P3 - why each phone has not answered.** The request list carries `who` (one line per target phone): "Not checked
  in since 10:02 PM", "Old app 3.0.100 · update to answer", "No copy", "Sending parts", "Looking for the chat".
  Shown on the live card, the In progress card and the open request.
- **P4 - every phone on an old app.** All target phones below 3.0.101 and no answer after 30 min: "Old app on 2 phones ·
  Update LinkUp on these phones · still open". The request stays open (no auto-cancel).
- **E1 - Live requests row.** 1 request = one wide card; 2 = halves; 3+ = 3 in a row with ‹ › arrows in the header + dots.
- One small table made on first use (`admin_action_checkin_310`, no migration).

## Files (8 + 2 docs)
- new `internal/httpapi/admin_chat_310.go`, new `internal/httpapi/admin_chat_310_test.go`, new `internal/httpapi/testdata/conv_310_smoke.js`
- `internal/httpapi/admin_chat_221.go` (list: `who`, `stuck`)
- `internal/httpapi/admin_actions_220.go` (sync = check-in time + build per phone)
- `internal/httpapi/websocket.go` (connect nudge)
- `internal/httpapi/adminweb/views.js`, `internal/httpapi/adminweb/styles.css`
- `docs/history/TODO_310.md`, `docs/history/APPLY_backend_310.md` (git add -f)

## Tests (run here, Linux)
- `go build ./... && go vet ./...` EXIT 0. `gofmt -l`: none of the 310 files (5 old files listed, unchanged).
- Full `go test ./...`: all packages ok (cmd/server, discovery, httpapi, mdns299, store, tlsauto, ws). EXIT 0.
- New: `TestWhyEachPhoneWaits310`, `TestOldAppOnEveryPhoneAfter30Min310`, `TestConnectHintOnlyForTargetsAndThrottled310`,
  `TestAppVersion310`, `TestAdminWebConversations310`, `TestAdminWebConversationsSmoke310` (node: 29 checks, SMOKE_OK).
- Regression: `conv_307_smoke.js` SMOKE_OK; `go test -run '310|221|217|307|AdminWeb'` ok; old locks unchanged
  (`admin_web_221/217/307_test.go`, `admin_overview_test.go`).
- Look: real views.js + styles.css in Chromium, light + dark, 1 / 2 / 5 requests + old-app case, voice play, hover menu: no script errors.
- **NOT checked here:** real Windows server, real phones, real voice file from a phone.

## After the push
1. Restart the server.
2. Web > Conversations: open a chat with a voice note -> press the button -> it says getting, then plays.
3. kiran & ayesha request: each phone shows its own reason under the card.
