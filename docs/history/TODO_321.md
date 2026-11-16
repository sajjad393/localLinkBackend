# TODO 321 - Slow Chats / Conversations + server shown on/off (16 Nov 23:05-23:50 logs, 3.0.111+182)

Freeze ON: code only after "Build". Planned 3.0.112+183, app only (backend unchanged).
Proof files: `uploads/LinkUp-logs-RMX5303-2026-11-16-2345.txt` (Owner), `-2305`, `RMX3938-2345` (ayesha), `Z2453-2346` (sumaira); screenshots 234405, 234410, 234944, 235015, IMG-20261116-WA0037.

## Found
| # | What the user sees | Proof | Cause (code) |
|---|---|---|---|
| 1 | Chats and Conversations load slowly on the Owner phone | Owner log: `database has been locked` x573 in 1 h; `STORE_OP_SLOW op=outbox_read` x153, max 15,447 ms (STORE_SUMMARY 23:11:36); `ADMIN_PROGRESS_SLOW` up to 8,682 ms (22:53:02) with `parts=1761..1786` | `SqlAnswerPartStore.metas()` (`hidden_store_221.dart` l.778) is meant to be a light read but it reads every saved part completely. Its `own` and `at` columns sit after `body` / `sig` in the table (l.78-83), and it also asks for `length(body)+length(sig)`. So every call walks all ~1,786 parts (~30 KB each, tens of MB) through SQLCipher. Messages use the same database (`store.hide221`), so outbox reads, the chat list and chat screens wait behind it. Sandbox test (plain SQLite, 1,786 x 35 KB): 55 ms vs 6 ms without those columns (9x); on the phone with SQLCipher that becomes seconds. |
| 2 | Conversations very slow even when nothing is coming | code | `ChatRequests221.requests()` (`chat_answer_reader.dart` l.1044) calls `progressOf(id)` for EVERY saved request. Each one is another full `metas()` read (carrier l.1186), so N requests = N full reads. |
| 3 | Owner keeps 1,786 answer parts | `parts=1786` | `ChatAnswerReader.sweep()` (l.455) only drops copies that were OPENED more than 7 days ago. An answer never opened (or never closed) stays forever. |
| 4 | "Server connected" on one screen / phone, "No server" on another | Owner: from 23:13:35 `HTTP ... _ClientSocketException`, 23:23:50 `No route to host` to 192.168.18.27, but the socket logged no `WS_CLOSED` until 23:33:45 (`WS_STREAM_DONE`). Between 23:13 and 23:33 there were 0 `WS_FRAME_RECEIVED` but 100+ `WS_SEND_SUCCESS` | The app websocket (`websocket_service.dart` l.149 `IOWebSocketChannel.connect`) has no ping. A dead link is only noticed when TCP gives up (~20 min). During that time `socket.isConnected` stays true. Conversations (`serverOnline: () => socket.isConnected`, app_dependencies l.1128) then waits up to 30 s for `/admin/chat-requests` (`HttpChatAdminServer._timeout` 30 s, l.167) on every load. The server already answers pings (`websocket.go` l.433). |
| 5 | Owner map "Server Off" while ayesha's map shows the server connected (23:44) | Owner 23:40-23:45: every connect to 192.168.18.27:8080 times out (`WS_RETRY_TIMEOUT`, `HTTP_REQUEST_FAILED 6002ms`); ayesha `WS_READY` 23:43:06; ZTE `WS_READY` 22:54:46 | **Not a map bug.** Each phone draws ITS OWN link to the server, and the Owner's Wi-Fi really could not reach the server PC at that time. Likely cause: the Owner hosts the Wi-Fi Direct group (`groupOwner=true`; `group create ... band=2.4` 22:55:13), so its Wi-Fi radio shares time between the router and the group. NOT VERIFIED (no router info). |
| 6 | Sync error | Owner 22:57:38 `ADMIN_ACT_SYNC_SKIP Concurrent modification during iteration: _Map len:39` (a nearby `admin_act_parts` came in at 22:57:37.895) | `admin_action_service.dart` l.1434 `for (e in _entries.values.where(..)) { await _save(e); }`. A card arriving from Nearby during the await changes `_entries`, so that sync round stops. |
| 7 | ayesha's "My links" shows her as "Y" (from "You") | WA0037 | `home_screen.dart` l.1371 `MyLinksCard273(...)` passes no `selfName`, so it is 'You' and `noteYouName` ignores 'You' (admin_kit216 l.1715). Without a photo, the letter comes from "You". |

Also in the logs (no fix):
- 22:46 the Owner was still on build 180 while the minimum was 182 (`VERSION_SELF_STOPPED`, 426), then updated at 22:48.
- The server PC clock is 3 h 37 min behind (`SERVER_CLOCK_WRONG offset_s=-13044`); the banner stays hidden (315).
- Owner `PUT /device-key -> 409 already bound` x2 (old rule, 2 tries only).

## Added 16 Nov - "chat is ready but photos / videos don't show, voice notes keep loading"
"Ready" means the TEXT of the chat is here. Photos / videos / voice notes come afterwards by themselves (317: one Get file card per file, max 2 on the way). Logs: the Owner made **0 file requests** after 11:46 (last `ADMIN_ACT_MADE action=get_file` 11:46:20 on 3.0.107; no `FILE_QUEUE_ASKED` in 1901 / 2305 / 2345). The 2 plans made tonight had `files=0` (new-only, up to date).
| # | Cause (code) | Where |
|---|---|---|
| 8 | A file that was asked but never arrives stays "on the way" forever: no retry, no give-up. With 2 such files, the queue never asks the next one, so the rest of the chat's photos / videos / voice stay "Next in line". The old manual Get file cards from 11:37-11:46 (3.0.107) can be such rows (`43f579` never finished; the others were "finished" = the phone answered, but there's no proof their parts arrived). | `file_queue_317.dart` `_stateOf` l.435/438, window l.400-404 |
| 9 | A voice note kept on the phone (318 kept copy) never plays: the voice row needs a ready Get file row (`_files310`) and ignores the "kept here" state that photos use, so it shows the spinner / "Next in line" forever. | viewer `_voice310Row` l.1361-1396 vs `_view317` l.288-301 |
| 10 | The viewer only knows which files are here after `_loadFiles310` -> `requests()` (= #2: one full parts read per request + up to 30 s server wait), and the bytes come from the same jammed database (#1). Files that are here show late. | viewer l.127-150 |
| 11 | A Get file row from the server list (`offline=false`): while the socket says online, bytes come ONLY from the server (no local fallback). If the file came by Nearby, the photo stays as the thumbnail / grey box, and the screen asks again on every redraw (`_bytesOf317` called from build). | `fileBytes` l.1546-1553; viewer l.1226-1227 |

Plan (add):
- F7: a file "on the way" with no new part for 10 min is asked again once (new card, same file), then marked "Not on the phone any more" after a 2nd fail; old pre-317 manual rows don't count in the window.
- F8: voice row uses the same `_view317` state (kept / here -> plays from the kept bytes).
- F9: the viewer reads the files of THIS chat directly (no `requests()` call).
- F11: one server status for every screen: "connected" only after the server answered; "off" only after 15 s without an answer (no flip-flop every few seconds while the socket retries 4-30 s). Mini map, Admin, Link paths and the Conversations pill all read it.
- F10: `fileBytes` falls back to the parts on this phone when the server has nothing; one try per file per screen (no ask on every redraw).

## Added 16 Nov - user: "Ready means ALL available" + why shimmer takes so long
User rule: **Ready = text + every photo / video / voice note here.** If a file is not here, show a clear label instead of a grey box / spinner.
| # | Change | Where |
|---|---|---|
| R1 | List status: green "Ready to read" only when the text AND all files are here. Text here + files still coming = blue "Messages here · 3 of 7 files coming" (counts as Coming). Files that can't come = grey "2 files not available" under it. | `admin_conversations_screen.dart` `_rowOf320` + FileQueue317 trips |
| R2 | Viewer: every file that is not here = a clear label card (icon + words), no grey box / endless spinner: "Photo · pending - comes by itself", "Video · pending", "Voice note · pending"; can't come = "Photo · not available (deleted on the phone)" / "too big" / "stopped". Here = the real photo / video / playable voice. | viewer `_attachment`, `_voice310Row` |
| R3 | Viewer top strip until all files are here: "Messages here · files 3 of 7"; then "Everything is here" for 3 s. | viewer |

Shimmer = the screen waits for its FIRST data. What each one waits for:
| # | Screen | Waits for | Proof |
|---|---|---|---|
| 12 | Chats list | the SERVER list first (`repository.devices()` = `_api.devices()`, then `groups()`), only after it fails the saved list (`home_bloc.dart` l.121-175). Server unreachable = 6 s timeout each; dead connection = longer. Then one database write per contact (`cacheDevice`) behind the jam. | `HTTP_REQUEST_FAILED ... (6002ms)`; home_bloc l.139-152 |
| 13 | Messages screen | saved messages first (good), but that read waits behind the database jam (#1). A chat with nothing saved waits for `synchronize()` + `api.messages` + decrypt + save. | `MESSAGE_HISTORY_START` -> `SUCCESS`: 3.3 s, 8.1 s, 9.0 s, 16.6 s for 2-3 messages (22:48:51-22:50:06) |
| 14 | Conversations (admin) | `online()` + `conversations()` (server) + `requests()` (#2: one full parts read per request + up to 30 s server wait) - all 3 before the first paint. | `admin_conversations_screen.dart` `_load` l.104-120 |

Plan (add):
- F12: Chats shows the saved list at once; the server list updates it behind (no shimmer when anything is saved).
- F13: Conversations paints saved requests + kept copies at once; server chats / requests fill in behind (8 s max).
- (F1 / F2 / F4 remove the database jam and the 30 s / 20 min waits for all 3 screens.)

## Plan
| # | Fix | Where |
|---|---|---|
| F1 | A light row table `answer_meta` (no body): filled once from `answer_parts` (one read at the first start), kept in step by put / remove / dropAnswer; plus the rows in memory (like the 319 acks). `metas()` never touches bodies again. | `hidden_store_221.dart` |
| F2 | Conversations takes progress from the counted map (`ChatAnswerCarrier.progress`) instead of one `progressOf` per request. | `chat_answer_reader.dart`, `app_dependencies.dart` |
| F3 | Answers never opened also go after 7 days (same as opened ones; the 318 kept copy is not touched). | `chat_answer_reader.dart` sweep - **needs user OK** |
| F4 | Websocket ping every 20 s: a dead link closes in under a minute instead of 20 min; the server pill / map / Conversations then agree. Conversations server list timeout 30 s -> 8 s. | `websocket_service.dart`, `chat_answer_reader.dart` |
| F5 | Copy the list before the awaits in the sync loop. | `admin_action_service.dart` |
| F6 | Home "My links" gets my own name -> "A", not "Y". | `home_screen.dart` |

## Tests (planned)
- F1: `metas()` does not read `body`/`sig` (fake DB records the columns asked for); the memory rows stay right after put / remove / drop / restart; 2,000 parts -> recount reads no blob.
- F2: 30 saved requests -> 1 parts read, not 30.
- F3: a never-opened answer 8 days old goes; 6 days old stays; a kept copy stays.
- F4: the channel is made with `pingInterval` 20 s; no reply -> closed -> `serverConnected` false.
- F5: a card added during sync -> no error, all saved.
- F6: the own node shows the own initial.
- Regression: bundle 316 / 317 / 318 / 319 / 320 carrier + viewer + Conversations tests, websocket tests, the full suite.

## Added 16 Nov - user: "complete plan to avoid any misleading errors; login via server failed; no log access on the login screen; make the app smooth"
The failed login is in NONE of the uploaded logs (no `AUTH_` lines in RMX5303-2345, RMX3938-2345, Z2453-2346). **Real cause NOT VERIFIED.** Below = what the code does today that can hide or twist the real reason (all read on `56cffbc`).

### Found (login)
| # | Problem | Where (code) |
|---|---|---|
| 15 | No way to share logs before sign-in. Log share is only the 5 fast taps on "Chats" (Home). Welcome and Sign-in / Create account have none. `LogShareService.prepare()` needs no account (reads the log folder only), so it can work there. | `home_screen.dart` l.180-193 (only caller of `runLogShare`); `log_share_service.dart` l.71-101 |
| 16 | **A server failure can quietly become an offline sign-in.** Every server check failure is a `StateError` and every login timeout (10 s) / socket error counts as "offline". If this phone has a saved copy of the account, the app opens WITHOUT the server and says nothing, so it looks like "signed in" but the server never accepted it. If it has no saved copy, the user sees "This phone has no saved sign-in yet. Turn the server on and sign in once." even when the server PC IS on (e.g. it was found but did not answer in time). | `authentication_bloc.dart` l.137-147, `_isOfflineEligible` l.254-265; `locallink_api.dart` l.450 (10 s); `setup_screen.dart` l.97-134 (offline = straight in, no words) |
| 17 | **One sentence hides many reasons.** Any error text containing "server", "network", "connection" or "socket" becomes "Could not connect to LinkUp. Make sure the LinkUp server is available on your private network." This also happens for a server that WAS reached and answered "Server error (500)" or "failed to ...". | `authentication_bloc.dart` `_cleanAuthenticationError` l.268-277 |
| 18 | Raw English server words reach the screen (no Urdu, not plain): "invalid username or password", "account temporarily locked after repeated failed logins", "authentication rate limit exceeded", "device pairing is required for this first login", "device registration conflict", "account is not active", "device is revoked and cannot re-authenticate", "stored account credentials are invalid". | backend `auth.go` `LoginAccount` (writeError lines); app `locallink_api.dart` `_error` l.1525 |
| 19 | Wrong promise: a timeout says "The server took too long to respond. It will keep retrying automatically." The sign-in screen never retries. | `bloc_utils.dart` l.25-26 |
| 20 | The steps "search" / "found" are set but no screen shows them, so the user sees one spinner for up to 5 s (search) + 6 s per server address tried + 10 s (login). | bloc l.110/117; no `stage` in `features/authentication/presentation` |
| 21 | A failed try can change the saved server address: every address tried is saved BEFORE it is asked. After a full failure, the saved address = the LAST one tried (could be an old address or one a nearby phone told). On phones where search does not work (Android 10/11 `.local`), the real server is then not the first one tried next time. NOT VERIFIED that this happened. | `authentication_repository.dart` `verifyServer` l.55-66, loop l.132-173 |
| 22 | The log says too little: `AUTH_RESULT what=sign_in mode=none reason=error` - no step, no error type, no HTTP code, no count of addresses tried, no time. The setup screen catch is `catch (_) {}`. | bloc l.126/135/144/147; `setup_screen.dart` l.135 |

### Plan (login) - L1-L8
| # | Fix |
|---|---|
| L1 | **Clear error = plain words + a short code** (en + ur), shown under the button. Code line under it in small grey, e.g. `Code L-02 · 192.168.18.27` (no password, no username, no key). Table below. |
| L2 | **Offline sign-in is never silent** (user pick, see question): when the server did not answer and the app opens from the saved copy, say it once. A server that ANSWERED with a "no" (wrong password, locked, removed, identity changed, pairing needed, update needed) never opens offline (256 A5 kept). |
| L3 | **Show the step** under the button: "Looking for the server…" -> "Server found · 192.168.18.27 · checking…" -> "Signing in…". |
| L4 | **Logs on the login screen (user pick 17 Nov):** 5 fast taps on the app bar title "Log in" (same screen shows "Create account") = same "Preparing logs" + share sheet as Home (`TapBurstCounter` + `runLogShare`). Where: `setup_screen.dart` l.374-376 `AppBar(title: Text(register ? L.t.createAccount : L.t.logIn))`. No visible link, no pop-up. Works with no account (`LogShareService.prepare` reads the log folder only). |
| L5 | **Log lines** (no secrets): `AUTH_CANDIDATES saved=1 found=N hint=0/1`; `AUTH_VERIFY addr=… result=ok/timeout/refused/no_route/key_changed/bad_proof/hint_ignored ms=…`; `AUTH_RESULT what=sign_in mode=server/offline/none code=L-0x status=401 err=<type> stage=search/verify/login ms=…`; offline open adds `why=<code>`. |
| L6 | Keep the old saved server address unless a new one passes the check (no change on failure). |
| L7 | Remove the blanket rewrite (#17) and the "keeps retrying" words on the sign-in screen (#19); every server "no" is mapped by status / error_code, never by matching words. |
| L8 | (my advice) Ask the saved address and search at the same time; the first one that passes wins. Worst wait from ~33 s to ~16 s. |

| Code | When | Words (en) |
|---|---|---|
| L-01 | Search found no server and no saved address | No LinkUp server found on this Wi-Fi. Check that the server PC is on and this phone is on the same Wi-Fi. |
| L-02 | Server address known but it does not answer (timeout / refused / no route) | The server at 192.168.18.27 is not answering. Check the server PC and the Wi-Fi, then try again. |
| L-03 | 401 BAD_LOGIN | Wrong username or password. |
| L-04 | 429 account locked | Too many wrong tries. This account is locked for a few minutes. |
| L-05 | 429 rate limit | Too many tries from this phone. Wait 1 minute and try again. |
| L-06 | Server identity changed / bad proof (ServerProblem256) | This server is not the one this phone knows. Ask the admin (plain 409 words, no auto-change - rule kept). |
| L-07 | 403 PAIRING_REQUIRED | This phone must be paired by the admin first. |
| L-08 | 403 device revoked | This phone was removed by the admin. |
| L-09 | 403 account not active | This account is turned off. Ask the admin. |
| L-10 | 426 | Update LinkUp to sign in (shows the needed version). Login uses plain `http.post` (not the 426 client) - check and map it. |
| L-11 | 5xx / 409 device conflict | The server had a problem (code 500). Try again; if it repeats, share logs. |
| L-12 | Server found, then the login itself timed out (10 s) | The server was found but did not finish signing in. Try again. |
| L-13 | Opened from the saved copy (offline) | Signed in on this phone only - server not answering (L-02). It connects by itself later. |

### Smooth - S1-S4 (adds to F1-F13)
| # | Proof (23:45 logs) | Fix |
|---|---|---|
| S1 | The database jam is NOT only on the Owner phone: ayesha `database has been locked` x45, `STORE_OP_SLOW outbox_read` x10 up to 12,205 ms, `ADMIN_PROGRESS_SLOW` 5,389 / 5,762 ms with parts 2,483 / 2,309. Owner x573. | F1 + F2 fix both phones (same `metas()` read). |
| S2 | Start-up "database" step 2,936 ms (Owner 22:48:12, right after the 22:48 update) and 1,992 ms (ayesha 23:42:02); normal starts 122-249 ms. Looks like a one-time upgrade after install (NOT VERIFIED for ayesha). | No change to start-up. The F1 one-time fill of `answer_meta` runs AFTER the first screen, never inside the database step. Add `STARTUP_DB_UPGRADE from=… to=… ms=…`. |
| S3 | Chats / Conversations / messages shimmer (#12-#14), dead link 20 min (#4) | F12, F13, F4, F9 as planned. |
| S4 | Skipped frames: Owner 1x54, ayesha 1x105, sumaira max 24 - only at start. | No fix. |

### Tests (planned, login + smooth)
- L1/L7: one test per code L-01..L-13 (fake repository throws each case) -> exact words + code; a 500 is never shown as "could not connect"; a timeout never says "keeps retrying".
- L2: server answered 401 -> no offline open; timeout + saved copy -> offline open + L-13 note once; no saved copy + timeout -> L-02 words (not "no saved sign-in").
- L3: stage words change search -> verify -> login.
- L4: 5 fast taps on the "Log in" / "Create account" app bar title open the log dialog (fake prepare/share); 4 taps or slow taps do nothing; works with no account; no visible link.
- L5: log lines carry code/stage/status and never the password, username or key.
- L6: a failed try keeps the old saved address.
- L8: saved and search at the same time; first pass wins; a hint with a bad key tail is still ignored.
- S2: `answer_meta` fill not inside the database step.
- Regression: bundle 110 / 223 / 231 / 256 / 265 / 278 auth tests, bundle 180 log share tests, the full suite.

## Added 17 Nov - "server is up but Forgot password says Offline" (screenshot 20261117-004550, log RMX5303-2026-11-17-0047)
**VERIFIED in log + code.** The server WAS up: 1 min later the sign-in found it in 279 ms (`SERVER_SEARCH found addr=http://192.168.18.27:8080 ms=279` 00:46:46) and signed in (`AUTH_RESULT what=sign_in mode=server` 00:46:49).
The log starts fresh at 00:45:25 (`BOOT_ENGINE_IDLE reason=noAccount`, `SERVER_FIRST_TRUST` 00:46:47, database step 2,493 ms) = app data was new (cleared / fresh install - NOT VERIFIED which). So no server address was saved yet.
From 00:45:25 to 00:46:29 (Forgot password open, screenshot 00:45:50) there is **no SERVER_SEARCH and no HTTP line at all**: the screen never asked the network.
| # | Cause (code) | Where |
|---|---|---|
| 23 | `serverOnline222()` returns **false at once when no server address is saved** (`if (saved.isEmpty) return false;`) - no search. Forgot password then shows "Offline · works on this phone", and "Ask the admin" refuses with "This needs the server…" without trying (`if (state.online == false) return _fail(...)`). The real send (`createNoCodeRequest222` -> `prepareForRecovery`) DOES search and would have found the server. The check runs once when the screen opens (no re-check). Same check on the Confirm account (235) screen. Nothing is logged. | `recovery_repository.dart` l.181-194; `account_recovery_bloc.dart` l.403, l.419-422, l.486 (askAdmin), l.460 (reset with code -> offline reset when `online` false); `account_return_235.dart` l.59-65; `confirm_account_235_screen.dart` l.97-113 |
| 24 | Reset with a code on that screen also goes the OFFLINE way (`resetOffline222`) because of the same false "offline". | `account_recovery_bloc.dart` l.460-467 |
| 25 | Two notes say the same thing (red error + "Needs the server." box). | `account_recovery_screen.dart` l.470 |
Side note (no fix, rule kept: no auto-rotate): 00:46:57 `PUT /device-key -> 409 identity key is already bound` - the new app data made a new key; the server keeps the old one.

Plan (add):
- L9: one server check for Forgot password + Confirm account = the same as sign-in (saved address AND search). While it runs: grey pill "Checking server…". Re-check every 20 s and when the app comes back to the front.
- L10: "Ask the admin" and "Reset with code" never trust an old check: tap = search + send. Offline reset only after a FRESH search found nothing. Errors use the L codes (L-01 / L-02 …).
- L11: one error note only (drop the duplicate box when the red error shows).
- L12: log `RECOVERY_SERVER_CHECK saved=0/1 found=N result=ok/none/timeout/key_changed ms=…`.
Tests: no saved address + server found by search -> pill online, Ask the admin sends; search finds nothing -> L-01 words; reset with code with a found server -> server reset (not offline); 20 s re-check flips Offline -> online; 235 screen same; regression bundle 222 / 235 / 290 recovery tests.

## Added 17 Nov - user: "Same issue is with login as well"
No log has a failed sign-in (the 0047 log starts at 00:45:25 with new app data; its only sign-in WORKED via the server at 00:46:49). **Real cause NOT VERIFIED.**
Code difference: sign-in DOES search (saved address + search + hint, `prepareForAuthentication`), unlike Forgot password (#23). So on sign-in "offline" means the server path really failed somewhere, but the words then blame the wrong thing:
| # | Words on screen | What it really means (code) |
|---|---|---|
| 26 | "This phone has no saved sign-in yet. Turn the server on and sign in once." | The server search / check / login failed (any network error, any 10 s timeout, or ANY other `StateError` from the check), then the offline try found no saved copy. |
| 26 | "That username and password do not match." | Same server failure, then the password did not match the copy saved on the PHONE. The server never saw it. (A real wrong password from the server shows "invalid username or password" in raw English.) |
| 26 | "This phone can sign in offline only with the last account used on it." | Same server failure; the saved copy is another account. |
| 26 | "Could not connect to LinkUp. Make sure…" | A server answer with an error (e.g. "Server error (500)") rewritten by `_cleanAuthenticationError`. |
Possible cause on the Owner phone (NOT VERIFIED): at 23:40-23:45 every Owner connect to 192.168.18.27 timed out while ayesha connected (#5); the Owner was hosting a Wi-Fi Direct group then. In the 0047 log (no group, `group plan=none reason=lan_first`) the server was found in 279 ms.

Plan (add):
- L13: when the server path failed, the words always start with the server reason + code, then the offline result: e.g. "The server at 192.168.18.27 is not answering (L-02). This phone has no saved sign-in to open offline." / "(L-02) Offline check: the password does not match the copy on this phone." A real server wrong-password = L-03 "Wrong username or password."
- L14: only network errors count as "no server" (not every `StateError`); any other check failure = L-11 with its own words + log line.
Tests: timeout + no copy -> L-02 + "no saved sign-in" words; timeout + wrong phone password -> L-02 + "copy on this phone" words (never plain "do not match"); 401 -> L-03; a non-network check failure -> L-11, no offline open.

## Added 17 Nov - screenshot 20261117-011908 (Log in: "This phone has no saved sign-in yet. Turn the server on and sign in once.")
No log for 01:19 (log share on the login screen = L4, not built yet).
**What the words prove (code, VERIFIED):** this text comes only from the offline fallback with reason `no_copy` (`offlineLoginReason278`, repo l.376-380: no `offlinePending` and no `offlineLoginAccount`). The offline fallback runs only for "offline-like" errors (#16). So at 01:19:
- the server never answered with a "no": a wrong password (401), a lock (429), a removed phone or a changed server key show OTHER words. **The password is not the problem.**
- the phone has NO saved copy of any account and (same reset) NO saved server address.
**Why nothing is saved and why no old logs:** "Reset setup" (Settings) -> `_resetAccount` (`local_link_app.dart` l.751-785) runs `clearConfiguration()` (`local_store.dart` l.1662-1677: server address, server key pin, offline copy, recovery code all deleted) AND `AppLogFile.instance.clearAll()` ("logging out removes the saved logs too", l.755). Clearing app data does the same. This is why the 0047 log starts at 00:45:25 and why no failed sign-in was ever in a log.
**So at 01:19 the only way to the server was the Wi-Fi search** (no saved address; hint only if a nearby LinkUp phone told one). Either the search got no answer (UDP 45454 broadcast) or the server was found but did not answer in time (6 s check / 10 s login). Which one: NOT VERIFIED.

Plan (add):
- L15: keep the LAST server address through "Reset setup" as a search candidate only (no trust kept: the key pin is still cleared, first contact is checked again like a search result). Then a missed search still tries 192.168.18.27.
- L16: a search with no answer asks once more (2nd round, 5 s) before giving up; log `SERVER_SEARCH none reason=no_reply rounds=2`.
- L17 (needs user OK): "Reset setup" keeps the logs of the last hour (logs have no messages / passwords) so a sign-in after a reset can still be explained. Today they are deleted.
- (L1/L13 then show "No LinkUp server answered on this Wi-Fi (L-01)" or "The server at … is not answering (L-02)" instead of "no saved sign-in".)
Tests: reset -> last address kept, pin cleared, still a candidate; search miss -> 2nd round; L17 reset keeps the log files (if OK).

## Added 17 Nov 01:25 - Chrome test on the Owner phone (screenshot 20261117-012504)
`http://192.168.18.27:8080/` in Chrome on the same phone -> **ERR_ADDRESS_UNREACHABLE** (= "no route to host": the phone gets no answer from that address on its Wi-Fi). The user says the server and web dashboard work (dashboard opens ON THE SERVER PC itself = PC address right, server runs; the phone path is not tested by that).
**VERIFIED: at 01:19-01:25 the failed sign-in was NOT an app bug** - Chrome could not reach the PC either. The network between this phone and the PC was down. Same as 16 Nov 23:23:50 (`No route to host` from the Owner only, while ayesha was connected).
Cause outside the app, NOT VERIFIED: the PC address changed (DHCP), the phone on another access point / band, router client isolation, or PC Wi-Fi power saving.
Plan (add):
- L18: split L-02 by the error: "no route / unreachable" -> "This phone can't reach the server PC (192.168.18.27) on this Wi-Fi. Check that the phone and the PC are on the same Wi-Fi, then try again (L-02a)"; "timeout" -> "The server PC is not answering (L-02b)"; "refused" -> "The server program is not running on 192.168.18.27 (L-02c)".
- Test: each socket error type -> its own words + code.

## Added 17 Nov - user: "same Wi-Fi, it worked before the last build"
- **319 + 320 changed no network / login / Wi-Fi / Android code** (GitHub compare e6473ed...d6c0cab and d6c0cab...56cffbc: only hidden_store_221, chat_answer_carrier, app_dependencies +1, admin viewer / Conversations screens, kit320, arb, app_build). VERIFIED.
- The SAME build 3.0.111+182 signed in via the server at 00:46:46 (search 279 ms). It failed at 01:19. Chrome failed at 01:25. After "clear storage" the app had no account and no Nearby permission, so at 01:25 it could not run Wi-Fi Direct. VERIFIED from logs + Android permission reset.
- Possible app trigger (NOT VERIFIED): after sign-in the app runs Wi-Fi Direct. On 16 Nov the Owner hosted a group (`group create band=2.4` 22:55:13) while it lost the server (23:13-23:45) and ayesha did not. A phone on 5 GHz Wi-Fi that hosts a 2.4 GHz group must share one radio, and the router link can starve. If the server drops, Nearby can start a group (`lan_first` only while the server answers), which can keep the server away = a loop.
- 01:39: phone on PTCL-BB 192.168.18.88, 150 Mbps (likely 2.4 GHz; then a group on another 2.4 GHz channel still shares the radio). Test asked: Wi-Fi off/on (or phone restart) -> Chrome; then sign in, use the app 10-15 min, Chrome again; Wi-Fi frequency (2.4 / 5 GHz).
- Candidate fix L19 (only if the test points to it): while this phone is on Wi-Fi with the server's subnet, no Wi-Fi Direct group on another band; after a server drop, wait 2 min + check Wi-Fi before starting a group; log `WD_GROUP_SKIP reason=on_server_wifi`.
- 17 Nov ~01:40: **user: after Wi-Fi off/on (or restart) the login works again.** VERIFIED: the phone's own Wi-Fi link was stuck (not the server, not the router for all, not the login code). Which trigger: NOT VERIFIED (Wi-Fi Direct group on the same radio, or a stale address entry after a random-MAC change). Next proof: if it breaks again, share logs from Home (5 taps on "Chats") BEFORE Wi-Fi off/on and WITHOUT clearing storage (clearing deletes logs).
- L19 (my advice, low risk, decide after that log): (a) no new Wi-Fi Direct group while this phone is on the server's Wi-Fi and the server answered in the last 2 min; (b) self-heal: if the server gives "no route" for 2 min while THIS app hosts / joins a group, the app leaves its own group once and tries the server again; log `WD_GROUP_DROP reason=server_no_route`. The app never turns Wi-Fi on/off (rule kept).

## User rule 17 Nov (after the 321 mock): "Wtf don't change ui like this" -> pick: **Conversations + chat: no new UI**
- No file cards, no files strip, no new status lines on Conversations / the chat viewer. R1/R2 become logic only with existing words (R1', R2' in PLAN_321.md); R3 dropped. Login + Forgot password UI changes (step line, code line, one message) still as planned (not rejected).
- Correction 17 Nov: user wants the NEW STATUS on admin Conversations (mock `mocks/n321/conversations_321.html`: same 320 rows/places/buttons; blue files coming / green Ready only when all here / grey 2 files not available). Chat viewer: no new UI (R2 cards + R3 strip dropped). Everything else in the plan unchanged.

## 17 Nov picks
- Messages screen redesign approved ('much better'): R4 per mocks/n321/messages_redesign_321.html.
- Notification option B ('B is good. Make sure i need same look and feel for every phone as is mock'): N1-N4 in PLAN_321 Phase 6b.
- D8 open: in 321 or separate build.
