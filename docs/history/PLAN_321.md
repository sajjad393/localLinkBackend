# PLAN 321 - complete plan in phases (all points discussed 16-17 Nov)

Version **3.0.112+183**, app + backend (R9 web owner page is IN 321, user 17 Nov: "Do all in one build").
Base: `56cffbc` (3.0.111+182). Every place below was read on that code.
Freeze ON: nothing is coded until you say **"Build"**. One app zip + one commit for the whole plan.
Details + proof lines: `TODO_321.md`. This file = the order of work.

Words used: **VERIFIED** = seen in logs/code. **NOT VERIFIED** = likely, not proven.

---

## Decisions I need from you (my advice in brackets)

| # | Question | My advice |
|---|---|---|
| D1 | F3: also delete chat copies that were **never opened** after 7 days (kept copies are never touched) | Yes |
| D2 | L2: when the app opens from the saved account because the server did not answer, how to tell you | Small note once on Home: "Signed in on this phone only - server not answering (L-02). It connects by itself later." |
| D3 | L17: "Reset setup" keeps the last hour of logs (no messages, no passwords in logs) | Yes |
| D4 | L19a: on the server's Wi-Fi, while the server answers, no NEW phone-to-phone group at all | Not now - only L19b (self-repair). L19a can stop friends who are not on Wi-Fi from getting messages. |
| D5 | L8: ask the saved server address and search the Wi-Fi at the same time (login wait ~33 s -> ~16 s) | Yes |
| D6 | R9: web owner page in the app style (needs a server change) | Separate build after 321 -> **user: in 321** |

Already decided by you: L4 = 5 fast taps on the "Log in" app bar title; "Ready" only when text AND all files are here; **chat screen: no UI change; Conversations: only the status words change (mock `conversations_321.html`).**

---

## Phase 1 - Login: true reason on screen + logs before sign-in
**Goal:** no misleading login message ever. Every failure says what really happened, with a short code, in English and Urdu, and you can share logs from the login screen.

| ID | Problem today (proof) | Change | Code place |
|---|---|---|---|
| L4 | No log share before sign-in. Only Home "Chats" x5 (`home_screen.dart` l.180-193). | 5 fast taps on the app bar title "Log in" / "Create account" -> same "Preparing logs" + share sheet. Works with no account (`LogShareService.prepare` reads only the log folder). | `setup_screen.dart` l.374-376; `TapBurstCounter`; `runLogShare` |
| L5 | `AUTH_RESULT` says only `mode=none reason=error`. | New lines (no password / username / key): `AUTH_CANDIDATES saved=0/1 found=N hint=0/1`, `AUTH_VERIFY addr=… result=ok/timeout/refused/no_route/key_changed/bad_proof/hint_ignored ms=…`, `AUTH_RESULT … code=L-xx status=… err=<type> stage=search/verify/login ms=…` | `authentication_bloc.dart` l.110-160; `authentication_repository.dart` l.102-182 |
| L1 + L18 | One sentence hides many reasons (#17); raw English server words (#18). | Every failure -> plain words + code under the button, e.g. `Code L-02a · 192.168.18.27`. Table below. | `authentication_bloc.dart`; new `login_problem_321.dart`; arb en + ur |
| L7 | `_cleanAuthenticationError` turns any text with "server/network/connection/socket" into "Could not connect…" - also for a server that answered 500 (l.268-277). Timeout text says "It will keep retrying automatically" - login never retries (`bloc_utils.dart` l.25-26). | Remove the word-matching. Map by error TYPE and HTTP status / `error_code` only. Login timeout gets its own words (no "retrying"). | `authentication_bloc.dart` l.268; `bloc_utils.dart` (login path only) |
| L13 + L14 | "This phone has no saved sign-in yet. Turn the server on…", "That username and password do not match." appear when the SERVER failed (offline fallback), not because of the password (#16, #26; VERIFIED by your 01:19 screenshot + code `offlineLoginReason278` l.376-395). Any `StateError` counts as "no server" (l.254-256). | Words always start with the server reason, then the offline result: "The server at 192.168.18.27 is not answering (L-02b). This phone has no saved sign-in to open offline." Only real network errors count as "no server"; any other check failure = L-11, never an offline open. | `authentication_bloc.dart` `_isOfflineEligible`, `_offlineFallback`; `password_kdf.dart` `offlineLoginText278` (words only) |
| L2 | Offline open is silent (`setup_screen.dart` l.97-134). | As D2. A server that ANSWERED "no" never opens offline (rule 256 A5 kept). | `setup_screen.dart`; `home_screen.dart` (note once) |
| L3 | Steps "search/found" set but never shown (#20). | Line under the button: "Looking for the server…" -> "Server found · 192.168.18.27 · checking…" -> "Signing in…". | `setup_screen.dart`; bloc `stage` |
| L6 | Every address tried is saved BEFORE it is asked; a full failure leaves the LAST one saved (#21; `verifyServer` l.55-66). | Save the address only after it passes the check; on failure the old one stays. | `authentication_repository.dart` |
| L15 | "Reset setup" / clear storage deletes the server address (`clearConfiguration` `local_store.dart` l.1662-1677), so after a reset only the search can find the server. | Keep the last server address as a search candidate only (key pin still cleared; checked again like a search result). | `local_store.dart`; `local_link_app.dart` `_resetAccount` l.751-785 |
| L16 | One search round (3 asks in 5 s, `discovery_service.dart` l.161-220). | No answer -> one more round before giving up; log `SERVER_SEARCH none reason=no_reply rounds=2`. | `authentication_repository.dart` (calls search twice) |
| L8 | Saved address, then search, then hint - one after another (up to ~33 s). | (D5) saved + search at the same time; first that passes wins. Hint key-tail check kept. | `authentication_repository.dart` l.102-182 |
| L17 | Reset deletes all logs (`AppLogFile.instance.clearAll()`, `local_link_app.dart` l.755). That is why no failed login was ever in a log. | (D3) keep the last hour of logs through Reset. | `local_link_app.dart` |

**Codes (words in en + ur):**
| Code | When | Words |
|---|---|---|
| L-01 | Search found nothing, no saved / last address | No LinkUp server answered on this Wi-Fi. Check that the server PC is on and this phone is on the same Wi-Fi. |
| L-02a | "No route" / unreachable (your 01:25 Chrome case) | This phone can't reach the server PC (192.168.18.27) on this Wi-Fi. Turn Wi-Fi off and on, then try again. |
| L-02b | Timeout | The server PC is not answering. Try again. |
| L-02c | Connection refused | The server program is not running on 192.168.18.27. |
| L-03 | 401 BAD_LOGIN | Wrong username or password. |
| L-04 | 429 account locked | Too many wrong tries. This account is locked for a few minutes. |
| L-05 | 429 rate limit | Too many tries from this phone. Wait 1 minute. |
| L-06 | Server key changed / bad proof | This server is not the one this phone knows. Ask the admin. (no auto-change - rule kept) |
| L-07 | 403 PAIRING_REQUIRED | This phone must be paired by the admin first. |
| L-08 | 403 device revoked | This phone was removed by the admin. |
| L-09 | 403 account not active | This account is turned off. Ask the admin. |
| L-10 | 426 | Update LinkUp to sign in (needed version shown). Login uses plain `http.post` (`locallink_api.dart` l.437) - check and map. |
| L-11 | 5xx / 409 device conflict / other check failure | The server had a problem (code 500). Try again; if it repeats, share logs. |
| L-12 | Server found, login itself timed out (10 s, l.450) | The server was found but did not finish signing in. Try again. |
| L-13 | Opened from the saved account | Signed in on this phone only - server not answering. It connects by itself later. |

**Tests:** one per code (fake repository/HTTP); a 500 never shows "could not connect"; timeout never says "retrying"; 401 never opens offline; timeout + saved copy -> offline + L-13 once; no copy + timeout -> L-02b words (not "turn the server on"); stage words change; 5 taps open logs, 4 / slow taps don't; log lines carry code/stage and never password/username/key; failed try keeps the old address; reset keeps the last address but clears the pin; second search round; parallel search + hint key check. Regression: bundle 110 / 223 / 231 / 256 / 265 / 278 / 180 tests.

**You will see:** an exact reason + code, a step line while it works, logs from the login screen.

---

## Phase 2 - Forgot password + Confirm account: real server check
**Goal:** never "Offline" while the server is up. (VERIFIED 17 Nov 00:45: no network request at all on that screen; the server was found 1 min later in 279 ms.)

| ID | Problem today | Change | Code place |
|---|---|---|---|
| L9 | `serverOnline222()` returns false at once when no address is saved (`if (saved.isEmpty) return false;`) - no search; checked once when the screen opens. | Same check as login (saved/last address + search). Grey pill "Checking server…" while it runs. Re-check every 20 s and when the app comes back to the front. | `recovery_repository.dart` l.181-194; `account_recovery_bloc.dart` l.403, l.419-422; `account_return_235.dart` l.59-65; `confirm_account_235_screen.dart` l.97-121 |
| L10 | "Ask the admin" refuses without trying (`state.online == false`, l.486); reset with a code goes the OFFLINE way on the same false result (l.460-467). | Tap = search + send. Offline reset only after a FRESH search found nothing. Errors use the L codes. | `account_recovery_bloc.dart` l.455-505 |
| L11 | Red error + "Needs the server." box say the same thing (`account_recovery_screen.dart` l.470). | One note only. | `account_recovery_screen.dart` |
| L12 | Nothing logged. | `RECOVERY_SERVER_CHECK saved=0/1 found=N result=ok/none/timeout/key_changed ms=…` | `recovery_repository.dart` |

**Tests:** no saved address + server found -> online, Ask the admin sends; nothing found -> L-01; code reset with a found server -> server reset (not offline); 20 s re-check flips Offline -> online; Confirm account screen same. Regression: bundle 222 / 235 / 286 / 290 recovery tests.

---

## Phase 3 - Database jam (main cause of slow Chats / Conversations / messages)
**Goal:** no more "database has been locked" (Owner x573, ayesha x45 in 1 h) and no 12-15 s reads.

| ID | Problem today (proof) | Change | Code place |
|---|---|---|---|
| F1 | `SqlAnswerPartStore.metas()` is meant to be light but reads every saved part fully (columns after `body`, `length(body)+length(sig)`) - 1,786 parts on Owner, 2,483 on ayesha. Messages use the same database, so they wait behind it. Sandbox: 55 ms vs 6 ms (x9), seconds with SQLCipher. | New light table `answer_meta` (no body), filled once from `answer_parts` AFTER the first screen (never in the start-up database step), kept in step by put / remove / dropAnswer; rows kept in memory (like the 319 acks). `metas()` never touches bodies. | `hidden_store_221.dart` l.78-83, l.779 |
| F2 | Conversations calls `progressOf(id)` once per saved request = one full read each (`requests()` `chat_answer_reader.dart` l.1029; `progressOf` carrier l.1184; wired `app_dependencies.dart` l.1151). | One counted map for all requests (`ChatAnswerCarrier.progress`). | `chat_answer_reader.dart`, `chat_answer_carrier.dart`, `app_dependencies.dart` |
| F3 | `sweep()` (l.456) drops only copies OPENED > 7 days ago; never-opened answers stay forever. | (D1) also drop never-opened answers after 7 days. 318 kept copy untouched. | `chat_answer_reader.dart` |
| F5 | Sync stops with "Concurrent modification during iteration" (Owner 22:57:38): loop awaits `_save(e)` while a Nearby card changes the map. | Copy the list before the loop. | `admin_action_service.dart` l.1435 |
| S2 | Start-up "database" 2.9 s / 2.0 s only right after an update (normal 122-249 ms). | No change; add `STARTUP_DB_UPGRADE from=… to=… ms=…`. | start-up timer / store open |

**Tests:** `metas()` never asks for `body`/`sig` (fake DB records columns); memory rows right after put/remove/drop/restart; 2,000 parts -> no blob read; 30 requests -> 1 parts read; never-opened 8 days goes, 6 days stays, kept copy stays (if D1); card added during sync -> no error; answer_meta fill not inside the database step. Regression: bundle 316-320 carrier/reader/viewer/Conversations tests.

**You will see:** Chats, Conversations and messages open fast on Owner AND ayesha.

---

## Phase 4 - Server link: one truth on every screen + Wi-Fi Direct self-repair
**Goal:** server shown "connected" only when it really is; a dead link noticed in under 1 min (not 20 min); the phone's own group can't keep the server away.

| ID | Problem today (proof) | Change | Code place |
|---|---|---|---|
| F4 | Websocket has no ping (`websocket_service.dart` l.149 `IOWebSocketChannel.connect`). Owner 23:13-23:33: 0 frames in, 100+ "sent", still "connected". Conversations waits 30 s per load (`chat_answer_reader.dart` l.167 `_timeout = 30 s`; `serverOnline: () => socket.isConnected` `app_dependencies.dart` l.1128). Server already answers pings. | `pingInterval: 20 s` -> dead link closes in < 1 min. Conversations server timeout 30 s -> 8 s. | `websocket_service.dart`; `chat_answer_reader.dart` |
| F11 | Screens disagree (mini map, Admin, Link paths, Conversations pill read different things). | One status: "connected" only after the server answered; "off" only after 15 s without an answer (no flip-flop while the socket retries). All screens read it. | `app_dependencies.dart` (serverOnline lines 926/962/1023/1128/1281), `home_screen.dart`, `my_links_map_273.dart`, `admin_conversations_screen.dart` |
| L19b | 16 Nov: Owner joined a group at 22:49:43 (`plan=join reason=wanted_by_group`) and created one at 22:55:13 (`plan=create reason=host_left_no_new_host`, band 2.4) while ON the server Wi-Fi; it lost the server 23:13-23:45 while ayesha (no group) stayed connected. 17 Nov: Chrome on Owner -> ERR_ADDRESS_UNREACHABLE; Wi-Fi off/on fixed it. In `planNearbyGroup` (`nearby_group_planner.dart` l.384) steps 2-4 (l.568-637) run BEFORE the "on Wi-Fi -> lan_first" step (l.644). Link to the lost server: NOT VERIFIED. | Self-repair: if the server gives "no route"/timeout for 2 min while THIS phone hosts/joins a group, the app leaves its own group once, waits 15 min before hosting again on this Wi-Fi, and retries the server. Log `WD_GROUP_DROP reason=server_no_route`. The app never turns Wi-Fi on/off (rule kept). Mesh voice / 8 hops untouched. | `wifi_direct_fallback_service.dart` (l.654, l.1491 area), `nearby_group_planner.dart` (input flag only) |
| L19a | - | (D4) only if you say yes. | `nearby_group_planner.dart` |

**Tests:** channel made with ping 20 s; no reply -> closed -> status off within 15 s rule; Conversations gives up at 8 s; all screens read one status; group + 2 min no-route -> leave once + 15 min back-off + log; server OK -> group untouched. Regression: websocket tests, bundle 296 / 299 status tests, nearby planner tests, Kotlin suite.

---

## Phase 5 - Screens show saved data at once (no long shimmer)
**Goal:** shimmer only when there is truly nothing saved.

| ID | Problem today | Change | Code place |
|---|---|---|---|
| F12 | Chats waits for the SERVER list first (`repository.devices()` `home_bloc.dart` l.129, then groups), saved list only after a failure (6 s each); then one DB write per contact (`cacheDevice` l.152). | Saved list at once; server list updates behind; writes only for changed contacts. | `home_bloc.dart` l.121-175 |
| F13 | Conversations `_load` waits for `online()` + `conversations()` + `requests()` before the first paint (`admin_conversations_screen.dart` l.104-120). | Saved requests + kept copies at once; server chats/requests fill in behind (8 s max). | `admin_conversations_screen.dart` |
| F9 | Viewer learns which files are here only after `_loadFiles310` -> `requests()` (l.123-150). | Read only THIS chat's files directly. | `admin_chat_viewer_screen.dart`, `chat_answer_reader.dart` |
| F10 | Server-list file row reads bytes ONLY from the server while "online" (`fileBytes` l.1539-1553); asks again on every redraw. | Fall back to the parts on this phone; one try per file per screen. | `chat_answer_reader.dart`, viewer l.1212-1230 |

**Tests:** saved list painted before any server call; server list replaces it; no write for unchanged contacts; Conversations first paint without server; viewer files without `requests()`; local fallback once per file. Regression: bundle 254 / 307 / 313 / 314 / 320 Chats + Conversations tests.

---

## Phase 6 - Photos / videos / voice + the "Ready" rule (your rule) - chat screen: NO UI change; Conversations: new status words only
**User 17 Nov: the chat screen keeps the 320 look exactly (no file cards, no files strip). Conversations keeps the same rows/places/buttons; only the status words change (R1).** In the chat screen only words that already exist are used (`coming320` "Coming", `comingParts320` "Coming · X of Y parts", `comingByItself320` "Coming by itself", "Ready to read", the existing "file gone" look).

| ID | Problem today | Change (logic only) | Code place |
|---|---|---|---|
| F7 | A file "on the way" that never comes stays forever and blocks the 2-file window; old 3.0.107 manual Get file rows count in the window (`file_queue_317.dart` l.400-438). | No new part for 10 min -> ask once more; 2nd fail -> marked gone (shown with the existing "file gone" look). Old pre-317 rows don't count. | `file_queue_317.dart` |
| F8 | A kept voice note never plays: the voice row ignores the kept state photos use (`_voice310Row` l.1355-1396 vs `_view317`). | Voice row uses the same state as photos: here (or kept) -> plays; still coming -> its existing words "Coming from X's phone" + parts "12 / 40" (today it can stay on "Next in line" forever); can't come -> its existing "Not on the phone any more" / "Too big to bring" / "Stopped". No new widget. | `admin_chat_viewer_screen.dart` |
| R1 | List says "Ready to read" when only the text is here. | **User OK 17 Nov (mock `mocks/n321/conversations_321.html`): same rows / places / buttons, only the status words change:** blue "Messages here · 3 of 7 files coming" (counted under Coming), green "Ready to read" only when every file is here, grey 2nd line "2 files not available" (can't come). Filter counts follow. | `admin_conversations_screen.dart` `_rowOf320`; arb en + ur (2 new keys) |
| R2' | Missing photo can stay a thumbnail/grey box when it came by Nearby (F10), and a stuck file says "Coming" forever (F7). | Use only the existing pieces (317/320): card + "Coming by itself" tag + "Coming from X's phone" + parts bar while it comes; block icon + "Not on X's phone any more" / "Too big to bring (over 50 MB)" / "Stopped" when it can't. The existing top files strip (317 G8: "3 of 7 files · 2.1 of 9 MB", green "All files here" 3 s) stays as is. | viewer (logic) |
| ~~R3~~ | - | **Dropped** - the chat screen already has the 317 files strip (`_filesStrip317`). | - |

**Tests:** stuck file -> asked again after 10 min -> gone after 2nd fail; old rows not counted; kept voice plays; voice still coming shows "Coming by itself"; list status stays "Coming" until all files here/gone, then "Ready to read"; no new widget on the chat screen; Conversations rows keep their place and button. Regression: bundle 317 / 318 / 320 file + viewer tests, `bundle317_test` l.485 lock, `ChatRequest317Test.kt:90` lock, bundle320 A9.

---

## Phase 6b - New messages screen + notification B (user picks 17 Nov)

User: "This mock is much better" (`messages_redesign_321.html`) and "B is good. Make sure i need same look and feel for every phone as is mock." (`notification_short_321.html`, option B). This replaces the "no UI change" rule for the chat screen.

| # | What | Change | Files |
|---|---|---|---|
| R4 | Admin messages screen redesign | As mock `messages_redesign_321.html` (4 screens): header with icon line (kept copy = "Kept here until 24 Nov" in green); files card (icon tile, "3 of 7 files here", one segment per file, red Stop pill, green "All files here" slides away after 3 s); chat-coming card same style; photo/video coming = full-size blurred thumbnail + ring % + "Coming by itself" + parts + size chip + "from X's phone"; here = picture fills the bubble, time on the picture, video play + size chip; can't come = dark greyed preview + 🚫 + reason; voice = gradient play button, coming = mic + ring + "Coming · 12 / 40", gone = 🚫 + reason; day chip on card, WhatsApp new bar kept. Grey bubbles kept (310), no Get button (317), menu / long-press / open photo unchanged. Flutter = identical on every phone. | `admin_chat_viewer_screen.dart`, `viewer_bubbles310.dart`, arb en+ur |
| N1 | Notification option B | Title only: "ayesha & kiran · 3/7" / "sumaira · waiting" / "Pricesess ✓"; no text line; no buttons (tap opens the chat; Stop is in the chat). Large icon = thumbnail of the file coming (faces if none). Colours unchanged (blue #209DEB / orange #F59E0B / green #22C55E); only "Ready" sounds; waiting swipeable after 30 min. | `chat_request_notifier_307.dart`, `ChatRequestNotifier307.kt` |
| N2 | Android 16+ bar | ProgressStyle: one segment per file (min width), green point per file here, tracker icon = type coming (photo / video / mic / hourglass when waiting), start = sender initial, end = check. "3/7" status-bar chip kept. | `ChatRequestNotifier307.kt` |
| N3 | Android 15 and older = same look | No ProgressStyle there, so a custom card body (DecoratedCustomViewStyle + RemoteViews) draws the same title, picture and a bitmap of the same piece bar. Group summary "3 chats" + 1-2 words each. Limits (Android, not the app): card frame / header / corners come from each phone's system skin; the "3/7" status-bar chip exists only on Android 16+. | `ChatRequestNotifier307.kt`, new layout xml |
| N4 | Tests | 317/307/314 Kotlin + Dart tests that lock the Open/Stop buttons and old words get listed and updated (only these). | tests |

D7 is closed by B (words follow the list). **D8 (open):** R4 + N1-N4 in 321, or a separate build after 321? (my advice: separate build 3.0.113.)

## Phase 7 - Small fix
| ID | Problem | Change | Code place |
|---|---|---|---|
| F6 | Home "My links" shows "Y" (from "You") for ayesha (WA0037). | Pass my own name -> own initial. | `home_screen.dart` l.1371 `MyLinksCard273(` |

---

## Phase 8 - Finish, test, deliver
1. Urdu: all new words in `app_en.arb` + `app_ur.arb` (Noto Naskh, LTR).
2. Tests: new bundle321 tests per phase + full suite (chunks of 15) + `dart analyze` = base + Kotlin suite. Results in `TEST_STATUS.md` with file + time proof.
3. Version 3.0.112+183 (`app_build.dart`, `pubspec.yaml`).
4. One app zip `locallink_321_<date>.zip` (inner app zip) + PowerShell block (app only) + `APPLY_app_321.md` phone checks.
5. Status: READY / READY WITH KNOWN LIMITATIONS / NOT READY.

**Phone checks after install (Owner + ayesha + sumaira):**
1. Login with Wi-Fi off -> L-01/L-02 words + code (not "no saved sign-in"); 5 taps on "Log in" -> logs.
2. Wrong password with server on -> L-03.
3. Fresh storage -> Forgot password shows the server online, "Ask the admin" sends.
4. Chats / Conversations open with no long shimmer; no "database has been locked" in logs.
5. Stop the server program on the PC -> all screens show Off within ~1 min, and back on together.
6. A chat with files: list shows "Messages here · x of y files coming" until all arrive, then "Ready to read"; voice note plays; a missing photo shows the existing "Coming by itself" tag (no grey box).
7. ayesha "My links" shows her own letter.

---

## Notification (superseded 17 Nov by Phase 6b N1-N4)
The 317 files card stays: blue "Chat ready · files coming" (3 of 7 files · MB · from X's phone, bar, Stop files), orange "Chat ready · waiting for the next trip" (last parts N min ago), green "Chat ready · all files here" ("7 files here" / "6 files here · 1 not on the phone any more"). F7 changes only the end: a stuck file no longer keeps the orange card forever - after the 2nd failed ask it ends green "… · 1 not on the phone any more". Open question (D7): change the words only to match the new list status ("Messages here · files coming"; summary "ready" counts only chats with all files)?

## Not in 321
- R9 web owner page (D6, backend) - **done in 321**.
- Already in place, no change: grey bubbles, New request sheet (318), live notification card, sealing, calls, 319 fixes, mesh voice 8 hops, server clock banner hidden (315).
- Still open from before (not touched): TECNO crash, 309 G1/G4, "Friends" group request, `sendLockedOnly313` risk, phone checks 3.0.108-3.0.111.


Mock (17 Nov): `~/mocks/n321/messages_321.html` - viewer unchanged; F8 voice label is one line (11 px, 172 px wide): before the first part it says "Coming from X's phone", then the parts "12 / 40" (the existing label), never both on one line.
