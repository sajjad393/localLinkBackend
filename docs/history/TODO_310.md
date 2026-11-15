# TODO 310 - Web Conversations: chat looks like the app, stuck "Sent", empty space (analysis only, freeze ON)

Report (15 Nov, 10:22 PM): `uploads/image-1.png` (answer viewer), `uploads/image-2.png` (Conversations page), `uploads/image-3.png` (web Map).
Code: backend `25921b6` (307), app `52769c6` (308). 308 is pushed: zip = repo (14/14 files); CI was still running at check time.

## Issue 1 - Chat in the answer viewer does not look like the app chat
What the screenshot shows:
- Voice notes show as plain files: file icon + `voice_<long id>.m4a` + "Get full file" (no play button, no bars, no length).
- A ⋮ menu on EVERY bubble; the name on every bubble in a 2-person chat; the time at the top of the bubble.
- The app chat: voice bubble (play, bars 2.5 px, length), time at the bottom inside the bubble, menu by long-press only, no name in a 2-person chat.

Why (code):
- Web `views.js` `att221()` knows only two kinds: image -> "photo", anything else -> "file". Voice (`audio/*`, `voice_*.m4a`) falls into "file".
- The answer has no voice data: `chat_answer_builder.dart` `messageJson()` sends only `file_id, name, type, size, thumb`.
  The phone already knows each voice note's length + bars (`VoiceWaveBook`, Bundle 303) but does not send them.
- The app's own answer viewer (`admin_chat_viewer_screen.dart` l.622) has the same gap (voice = "Get file").
- `bubble221()` puts the name + time + ⋮ in a head row on every bubble.

Plan:
- **V1 (app)** answer JSON: a voice note gets `voice: {ms, bars}` (bars = 40 levels as 0-9 digits, from `VoiceWaveBook` when known; else only `ms` or nothing). Old phones send nothing: web falls back to a voice bubble without bars.
- **V2 (web)** `att221`: `audio/*` or `voice_*.m4a` = voice bubble like the app: round play button, bars, length.
  Tap play = "Get voice note" (the same get_file request as "Get full file"); when Ready it plays in the bubble.
  Photos keep thumbnails; other files keep the file row but with a short name (no long id).
- **V3 (web)** bubbles like the app: time at the bottom right inside the bubble; name only in groups (and on the first
  bubble of a run in a group); ⋮ shows on hover only (keyboard focus too); same colours as the app (v2 dark + light).
- **V4 (app)** the app answer viewer gets the same voice bubble.
- Mock first (dark + light, app match), then build.

## Issue 2 - "kiran & ayesha" stays at "Sent" for 20 min while both phones are connected
"Sent" = the server has nothing back from either phone: no receipt, no "seen" note, no answer part (`reachedPhones307`).

How a request reaches a phone (code):
- Server makes the card and sends ONE hint on the socket (`hintAdminActions`, only at creation, only to phones connected at that moment).
- A phone also checks every 10 min (`AdminActionRules.serverEvery`) and once at app start. No check when the socket reconnects.
- A phone that has the chat builds parts and uploads them. A phone WITHOUT the chat (`no_copy_here`) keeps a "seen: empty"
  note and uploads it only at its NEXT 10-min check (`_sendSeen` -> `_keepSeen307`, no sync right away).
- Uploading "seen" notes (so the request can close as "No messages") exists only from **3.0.101 (307)**.
  A phone on 3.0.100 or older never tells the server "I have no copy" -> the request stays "Sent" with no end.

Most likely cause (NOT VERIFIED - needs kiran's and ayesha's app version or their logs):
- If kiran and ayesha have no chat with each other and their phones are still on **3.0.100**, nothing can ever come back -> "Sent" forever.
- If they are on 3.0.101+: the answer can take up to ~10 min (next check), not 20. Then look for `ADMIN_ACT_SYNC_SKIP`
  (server check failing), `ADMIN_ACT_NOT_MINE` (card locked for another phone id of the same person), `ADMIN_ACT_DO_FAIL`.

Plan:
- **P1 (app)** after "no copy here" or after building parts: upload at once (sync right away), not at the next 10-min check.
- **P2 (app + backend)** the phone checks when its server socket (re)connects; the server sends the hint again to a target phone
  when it connects while a request for it is still open.
- **P3 (backend + web)** the live card tells WHY per phone, from data the server already has (`device_builds`, last check-in):
  "kiran · not checked in since the request" / "ayesha · old app 3.0.100: update to answer" / "kiran · no copy".
- **P4 (backend)** a request where every target phone is on an app older than 3.0.101 and has not answered in 30 min shows
  "Old app on these phones - update them" instead of an endless "Sent" (it stays open; no auto-cancel).

## Issue 3 - Too much empty space on the Conversations page
- The "Live requests" row is a carousel of fixed-width cards (~350 px). With ONE request, 2/3 of the row is empty (crossed area).
Plan:
- **E1 (web)** 1 request = one wide card across the row (people + step bar spread out); 2 = two halves; 3+ = 3 in a row, with ‹ › arrows in the "Live requests" header and dots (approved in the mock).
- Mock first with E1 (and Issue 1) in the same file.

## Not in 310
- The web Map (image-3) looks right: 4 phones, all on the server, sajjad-kiran / sajjad-ayesha Wi-Fi, sajjad-sumaira Wi-Fi Direct. No change.

## Tests (when built)
- Web: voice attachment -> voice bubble (play, bars, length); no `voice` data -> bubble without bars; image/file unchanged.
- App: answer JSON has `voice.ms` / `voice.bars` for a known note; unknown note -> no `voice`.
- App: no copy -> seen note uploaded at once (sync called); parts built -> sync called.
- App: socket reconnect -> one admin-actions check (not more than 1 per 30 s).
- Backend: target connects while its request is open -> hint sent; per-phone reason (old app / not checked in / no copy).
- Web: 1 / 2 / 3+ live cards layout.
- Regression: all 307 tests (backend + web + app) + full app suite + `go test ./...`.

## Mock (15 Nov)
- `~/mocks/conversations_310.html` (built by `~/mocks/m307/mk_mock_310.py`, dark + light, app colours read from the real LocalLinkTheme).
- Sections: 1 web viewer like the app Messages screen (+ group names), 2 voice states, 3 app viewer (V4, same bubbles), 4 live rows 1/2/3+ with per-phone reasons, 5 old-app state.
- Waiting for the user's OK / changes, then "Build".


## Status
BUILT 15 Nov: app 3.0.103+174 on `52769c6`, backend 310 on `25921b6`. Scope V1-V4, P1-P4, E1. See `APPLY_app_310.md` / `APPLY_backend_310.md`.
