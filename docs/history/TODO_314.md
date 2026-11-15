# TODO 314 - Get chat: not my own chats, one open request per chat, notification like the card

Status: BUILT (user "Build", 16 Nov). Base: app `e6bfc11` (3.0.106+177) / backend `93cd404`. App 3.0.107+178.
Source: `Screenshot_20261116-000636.png` (sheet), `-000615.png` / `-000610.png` (notifications),
`LinkUp-logs-RMX5303-2026-11-16-0018.txt` (C71, 3.0.105+176).

## What the C71 log shows (VERIFIED from the log)
- 23:52:20 group "Friends" made on the C71: `GROUP_NEARBY_CREATED members=1 origin=both`; card sent to ayesha (`to=d181c0 near=y`).
- 23:58:31 and 00:02:48 `ADMIN_ACT_CARD action=get_chat from=server`, each followed by `ADMIN_ACT_NOT_MINE`:
  the server's card reached the C71, and the C71 does not answer it.
- Nothing from ayesha's side is in this log (her phone is on 3.0.100+171).

## Issue 1 - chats I am in: not asked at all (app + backend + web) - USER PICK 16 Nov "hide"
Replaces the first plan (leave the owner's phone out of the targets): a chat the owner is in can't be
asked any more, so the owner's phone is never a target.
- Why it happened (VERIFIED in code): server `admin_chat_221.go` `pickChat()` case "group" targets ALL
  `group_members` devices (owner too); case "direct" adds the owner's phones when the owner is one of the two.
  Then 307 A2 ("all said no copy") can never finish and 310 P4 "Old app" never shows (the owner's phone is new).
- App, New request sheet (`admin_chat_request_sheet.dart`):
  - person picker: no "Me" row (today `people()` puts "Me" on top, `hidden_store_221.dart` l.957);
    the 307 note `request307_me_note` ("You already have this chat...") goes away;
  - group picker (USER PICK 16 Nov "all"): EVERY group I am not a member
    of: all groups the server knows (its `groups` table - a small new owner-only list: id, name, member
    count, me-in flag; the Chats list alone misses groups with no server messages yet) + groups this phone
    knows that I am not in. Hidden: only groups I am in. Row: "Friends · 3 members".
    + nearby groups reported by members (Issue 1c). Row of a reported one: "Study group · Nearby · 4 members".
- App, Chats tab (`admin_conversations_screen.dart` l.895): a row of a chat I am in stays, but has no
  "Get chat" button.
- Offline path (`chat_answer_reader.dart` `request()` / `_resolve()`): `meIn` -> refused with the same words.
- Server (`pickChat`): refuse when the owner's user is one of the two people / a member of the group:
  409 code `own_chat`, words "You are in this chat. It's already on your phone."
- Web (`adminweb/views.js` new request picker ~l.3262-3272): the same chats left out; the 409 words shown.
- Not affected: Delete completely, Get file, the 313 "Get chats back from friends".
- Lost on purpose: the other person's copy of a chat I am in (307 note). After a reinstall the 313 restore
  brings my chats back.

## Issue 1c - members report nearby groups to the admin - USER PICK 16 Nov "group info automatic, full chat only when I ask"
- Today (VERIFIED in code): a phone-only group goes on the server only when its OWNER's phone reaches the
  server (`group_nearby_service.dart` `bridge()`, called from `group_messaging_service.dart` l.663/978).
  Members never report it, so the admin (not a member) can't see or ask for it.
- New: every member phone sends a small signed "group note" for each active phone-only group it is in:
  {gid, name, member devices, made_at, from device}. No messages, no keys.
  - Server reachable: in the normal admin-actions sync; the server keeps it in a NEW admin-only table
    (not `groups`, nobody joins anything); the owner-only group list (Issue 1) includes them.
  - No server: to the admin's phone over the sealed link when near (sealed only, like 307 seen notes;
    relays may carry it). Kept on the admin phone.
  - Sent again when the name / members change, else at most once a day per group. Old apps: nothing.
- Get chat for a reported group: server knows it -> server card to its member devices (from the note);
  server off / not known -> the admin phone's offline card to those devices (existing offline path).
  A phone only answers for a group it is really in (existing check), so a fake note can't leak messages.
- Full chats are NOT sent by themselves; only with Get chat (same rule as every chat).
- Background (USER 16 Nov: "it can report me if we both or all phones are in background"): the note is
  sent and taken in the background too, by the same keep-alive that already runs the links and the admin
  sync with the app closed (`LocalLinkTransportService`, `stopWithTask=false`, keep-alive default ON;
  the C71 log shows `KEEP_ALIVE_STATE when=background` with links working). No new permission, no
  notification on either phone. Limits: a phone the system kills (ZTE `heartyservice`), "Force stop",
  or keep-alive turned off sends nothing until LinkUp runs again. Test: send + receive with both sides in
  the background path (no screen).
- Tests: note signed + sealed; stranger / unsigned note dropped; server list owner-only; admin list shows
  "Nearby"; Get chat targets = note devices; a non-member target answers nothing; once a day / on change.

## Issue 1b - one open request per chat - USER PICK 16 Nov "still blocked, cancel the old one first"
- Today: the Chats tab already shows the step chip instead of "Get chat" when a request is open
  (`admin_conversations_screen.dart` l.894-898). "+ New request" never checks, so the same chat is asked again.
- Rule: same chat (same 2 people in any order, or same group) with a request that is Sent / Reached /
  Coming -> no new request, ANY range. The sheet shows "Already asked · Open it" (opens that card); the
  Get chat button is off. Ready / cancelled / empty / closed do not block.
- Server: same rule in `AdminChatRequestCreate` -> 409 code `already_asked` + the open request id, words
  "This chat already has an open request. Open it or cancel it first." The app and web open that card.
- Offline path: the same check on the phone's remembered rows.
- Old duplicates stay (the user cancels them).
- Tests: app (picker lists, Chats row button, sheet "Already asked", offline refuse), backend (own_chat
  direct + group, already_asked any range, ready/cancelled don't block), web smoke.

## Issue 2 - why it stays pending (NOT VERIFIED)
- The only phone that can answer is ayesha's (3.0.100 = build 171, older than 3.0.101). That app cannot send
  "no copy" or per-phone reasons. Whether it got the card / answered is not in the C71 log.
- Check: web admin -> Conversations -> Requests -> this card: the line under ayesha says got / wait / old.
- After ayesha updates to 3.0.106 she answers or says why.
- Same for the 2h-old "ayesha & sumaira" request (both phones on old apps).

## Issue 3 - notification like the card (app, Android 16 ProgressStyle) - user wants
- Now: `ChatRequestNotifier307.kt` `plan()` uses `indeterminate=true` for "sent" -> a sliding line, no stops.
- Android limits: a Live notification can only use Android's ProgressStyle (no own drawing / animation,
  no custom views). Exact same animation is not possible.
- Mock: `mocks/n313/live_notification_mock.html` (light + dark).
  - A (my advice): no sliding line; stops at 33/66/100 like the card; start icon = chat bubble; end icon = check;
    the tracker = a ring in the step colour (orange Sent, blue Coming, green Ready); the bar is filled up to
    the tracker; the tracker moves when the step changes.
  - B: A + the tracker rocks while waiting (re-posts every few seconds) - not advised (battery, Android
    throttles, moves on the lock screen).
  - Android 15 and older: plain bar only (Android rule).
- USER PICK (16 Nov): **B** (A + the tracker rocks while waiting). This replaces the old "no lock-screen
  animations" rule for this notification only.
- B plan: Android 16+: ProgressStyle as A; while the step is "sent" or "reached" (nothing coming yet) the app
  re-posts the same notification with the tracker 4 points ahead / back every 3 s (`setOnlyAlertOnce`, no sound,
  same id). Stops when parts start coming (the real progress moves it), when ready / cancelled, and after
  10 min in the same step (then A, still) to save battery. Android 15 and older: the sliding line while waiting (USER PICK), filled bar once parts come.
- Files (3): `android/.../ChatRequestNotifier307.kt` (plan + ProgressStyle points/segments/icons + rock timer),
  `ChatRequest307Test.kt`; Dart card service only if the step data needs it. Backend: `admin_chat_221.go` + test.

## Code check of this plan (16 Nov, app `e6bfc11`, backend `93cd404`) - corrections
Read in code; these change the plan above:
- K1 (1b) The Chats tab does NOT fully stop doubles: the button is hidden when a request exists, but tapping
  the ROW still opens the sheet when the request has no parts yet (`admin_conversations_screen.dart` l.902
  `onTap: ... : () => _ask(pick)`). Fix there too: the row opens the open request's card.
- K2 (1b) The row lookup (l.130-140) matches any not-cancelled request (also done/ready). Keep it for the
  chip; the "blocked" rule uses only Sent / Reached / Coming (state pending and not ready).
- K3 (1) Server lists: the group list ALREADY exists - `GET /api/v1/admin/groups` (`admin.go` l.175-214:
  id, name, owner_id, created_at, member count; the web sheet uses it, `views.js` l.3249). No new list:
  add a `me` flag (the admin's user is a member) + the reported nearby groups (1c), and the app reads it.
  `GET /api/v1/admin/conversations` (`admin_conversations.go`) group rows have only a member count:
  add `me` there too (direct rows have device ids, the app can see its own device).
- K4 (1) "Me" on the server = `AdminAccount.UserID` (`admin_auth.go` l.47). It is set only for an app user
  made admin; a password-only web owner has no user -> nothing is "mine" for that login (nothing hidden).
- K5 (1) OFFLINE Chats tab: with no server it lists ONLY this phone's own chats and groups
  (`hidden_store_221.dart` `chats()` l.890-920 + `groups()`), i.e. all "mine". With the new rule every row
  loses Get chat -> the offline Chats tab would show nothing to ask.
  USER PICK 16 Nov: with no server the Chats tab shows the LAST SERVER LIST saved on the phone (saved after
  each good load, owner phone only, no message text - the list has none) + reported nearby groups (1c);
  my own chats/groups are not listed. A Get chat from it goes the offline way (cards to the phones).
- K6 (1c) A member phone knows who the owner is without a server: the server-signed crowns
  (`admin_badge.dart`, `AdminBadgeService` - passed phone to phone, role owner + user id); the owner's
  phone from the directory (one phone = one account). The note goes sealed to that phone when linked.
- K7 (1c) The answer side needs no server: a member builds a group answer from its own `group_messages`
  (`hidden_store_221.dart` `rows()` l.480). A phone not in the group has no rows -> "no copy".
- K8 (1c) The owner phone's offline card takes targets from its LOCAL `group_members`
  (`chat_answer_reader.dart` `_resolve()` l.680); a group I am not in is not there -> use the members from
  the note. Server: `pickChat()` needs the group in `groups` (else "group not found") -> a reported group
  takes its members from the note table.
- K9 (1c) IMPORTANT: the server reads `POST /api/v1/admin-actions/sync` with `DisallowUnknownFields`
  (`api.go` l.1163, `admin_actions_220.go` l.872). A new field there would make an OLD server refuse the
  whole sync (admin cards stop). So the notes get their OWN endpoint (`POST /api/v1/admin/group-notes`,
  device sign-in); an old server answers 404 -> the app just keeps the note. Backend first anyway.
- K10 (3) The rocking needs new drawables (ring tracker, check end icon; start icon = the chat bubble in
  use now `android.R.drawable.sym_action_chat`) and a timer in `ChatRequestNotifier307.kt` (Kotlin, main
  looper, runs while the process lives - same as the notifications now). Android 15 and older today:
  `buildCompat` `setProgress(100, p, indeterminate)` = the sliding line while Sent.
  USER PICK 16 Nov: keep the sliding line there while waiting (only moving bar Android allows); filled bar once parts come.
- K11 (new, from the log) "2h" on the notifications: `when` = the server's `made_at`
  (`chat_request_notifier_307.dart` l.255); the C71 log says `SERVER_CLOCK_WRONG offset_s=-9230 dir=slow`
  (server PC clock 2 h 34 min slow; Bundle 255 already warns). Fix the PC clock; AND (USER PICK 16 Nov "yes") the app corrects
  `when` by the known offset (`ServerClock255.wrong`) so the notification shows the right age.

## Files (planned)
- App: `admin_chat_request_sheet.dart`, `admin_conversations_screen.dart`, `chat_answer_reader.dart`,
  `hidden_store_221.dart` (only if the picker list needs a member flag), `ChatRequestNotifier307.kt`,
  `.arb` en + ur + gen, version 3.0.107+178, tests.
- App (1c): `group_nearby_service.dart` (note send), admin actions sync (carry), sealed handler on the admin phone.
- App (K1/K5/K10): Chats row tap, offline Chats list, new drawables `res/drawable/`.
- Backend: `internal/httpapi/admin_chat_221.go` (own_chat, already_asked, reported groups in pickChat), `admin.go` (groups `me` + reported), `admin_conversations.go` (`me`), NEW group-notes endpoint + table + `cmd/server/main.go` route, `adminweb/views.js`, tests (backend zip first).

## Regression (when built)
Full app suite + analyze (361 old, 0 new); backend `go vet` + `go test ./...`; phone: New request
(people / groups lists), Chats tab buttons, a second request for the same chat, the notification B on the C71.

## As built (16 Nov)
- Backend: new `internal/httpapi/admin_chat_314.go` (table `group_notes_314`, made on first use; `POST
  /api/v1/admin/group-notes`; own_chat / already_asked checks; `me` flags; reported groups in `pickChat`).
  The 409s come AFTER the date-range check (bad input stays 400). `admin.go` groups: `me`, `member_devices`
  (owner only), `nearby` (owner, first page). `admin_conversations.go`: `me` on rows. Web: own people / my
  groups left out of the picker, nearby groups listed, no Get chat on my rows, already_asked opens the card.
- App: `chat_answer_reader.dart` (`pickerPeople314`, `pickerGroups314`, `isOwnChat314`, `openRequestFor314`,
  saved server lists `admin_chats_saved_314` / `admin_groups_saved_314`, offline `conversations()`,
  `_resolve()` uses note members for a group I am not in), request sheet (no Me row, "Already asked · Open
  it", group rows "3 members" / "Nearby · 4 members"), Chats tab (K1: my rows "On your phone", no Get
  chat, no sheet; an open request row opens its card), NEW `group_notes_314.dart` (note send to the server
  + locked to the Owner phone, take on the Owner phone), wiring in `app_dependencies.dart`.
- Notification: `when` corrected by `ServerClock255.wrong` (not for requests made offline on the phone);
  Dart sends `rock_until` (= made + 10 min while Sent / Reached). Kotlin: start bubble, end check, ring
  tracker in the step colour, filled up to the tracker, rock +-4 every 3 s (main looper), stops at
  rock_until / next step / cancel. Android 15 and older: sliding line while Sent / Reached, bar once parts come.
- Old test changed (Issue 1, Me row removed): `test/bundle254_test.dart` picker test (Me row now absent;
  the "Me + noshad" send part removed because that chat is refused now).
