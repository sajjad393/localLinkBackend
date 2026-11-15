# TODO 317 - Get chat viewer + My links: "Ready" too early, raw group lines, file buttons, deleted look, map overlap

Status: TODO (freeze ON). Picks 16 Nov: C+D mock OK (mocks/n317/viewer_mock.html, files screen redrawn for G); G = everything comes by itself in 8 MB trips, no Get button (user pick); F yes (fix the start stall); H keep sides by phone. Goes in the same build as 315 + 316 (3.0.108+179). Base: app `5bad575`, backend `6db45b6`.
Sources: screenshots 16 Nov 11:17 / 11:38 / 11:39 (x3) / 11:45 + logs RMX3938 11:52, RMX5303 11:55.

## Screenshots (what I see)
| Shot | What it shows | Verdict |
|---|---|---|
| 11:17 My links (full map) | Clock banner (=315). "Drag to scroll · pinch to zoom" sits on top of ayesha's name; ayesha's circle is half under the zoom buttons | UI bug -> E |
| 11:38 ayesha & kiran, from ayesha's phone | Photo + 2 videos: grey "image" icon (also for videos), no picture, "Get full photo/video" buttons; the SAME video listed twice in one bubble; toast "Asked the phone for the full file" | D |
| 11:39 Pricesess "Full chat" (2 shots) | Empty page "Waiting for a phone of this chat. There is no time limit." while the card already said Ready. 2nd shot: the side panel is Android's volume slider (not LinkUp) | A |
| 11:39 Pricesess, from kiran's phone | First bubble is raw code `llev:v1:{"k":"create",...}` = the "kiran created the group" line. "hi" has no name. All bubbles grey on the left | B |
| 11:45 ayesha & kiran, from kiran's phone | 8 bubbles: outlined box + big red pill "Deleted by user · 16 Nov 11:41" each - heavy and repeated | C |

Side note (by design, bundle 255 B8): the answering phone's owner is first in the header and on the left, so the
same message is left in ayesha's copy and right in kiran's copy. Not changed unless you ask (question H).

## A - "Ready" before the chat is here (BUG, the question "why marked completed")
- Log C71: `ADMIN_ACT_MADE get_chat id=899efa 11:39:05` -> `ADMIN_ACT_DONE_IN result=ok 1/1 11:39:06` (kiran's phone said
  "I'm answering") -> `ADMIN_ANSWER_STOP parts=1 11:39:38` (the chat really arrived 32 s later).
- Code: `admin_action_service.dart` `_recount` sets `state='done'` when the phone's receipt arrives (need = 1);
  `request_step_307.dart` l.~120: `if ((of > 0 && parts >= of) || r.state == 'done')` -> **Ready with 0 parts**.
  The same step drives the Conversations card, the live row, the viewer card AND the Ready notification + buzz.
  Server rows: `done` with `parts < of` is also shown Ready (the done check comes before the parts check).
- Fix: for get_chat / get_file, Ready only when `of > 0 && parts >= of`. Phone answered but no parts yet ->
  the "Coming" step with 0 % ("kiran's phone answered · the chat is on the way"). Viewer: that text in place of
  "Waiting for a phone of this chat". Other actions (delete, block, rules) unchanged. No buzz before Ready.

## B - group system lines shown as raw code (app + web)
- `admin_chat_viewer_screen.dart` `_bubble` prints `body` as text; `adminweb/views.js` the same.
- Fix: a body that `GroupEvent.parse` reads becomes a centred grey line like the group chat
  (`GroupEvent.lines(..., nameOf: names from the event "nm" + the answer's names)`): "kiran created group Pricesess",
  "kiran added ayesha". The next bubble starts a new run (so "hi" gets the name "kiran"). Web: same words.

## C - deleted messages too heavy
- Now: transparent bubble + outline + red pill per message.
- Plan (needs a mock first, see question): normal bubble at 55 % strength + one small red line under the text
  "trash icon  Deleted 11:41" (date only when another day). Same in the web viewer.

## D - photo / video buttons
- Logs: C71 made 13 Get file in 9 min; 8 came back `refused:file_gone`, but the bubble kept "Get full video",
  so it was tapped again and again. Only voice notes have states (310).
- Fix: photos / videos / files get the voice states: "Asking..." with parts, then Open; `refused:file_gone` ->
  "Not on the phone any more" and no button (remembered per file). Video icon for videos.
- Same file twice in one bubble: cause NOT VERIFIED (the phone's attachment table has 2 rows). Fix: one row per
  `file_id` in the answer builder; the viewer also shows one row for the same name + size + type in one message.

## E - My links full map overlap (`link_paths_full_screen.dart`, not the sha-locked map file)
- The hint fades out after 4 s; the map keeps circles and names out of the zoom-button column and the hint line.

## F - start stall on ayesha (log, optional)
- `MAIN_STALL ms=756..2481 during=open at File.listFiles <- LocalLinkTransportService.onCreate` (4 times):
  `loadSeenMeshPackets()` reads every `.seen` file on the main thread at start (one file per received packet).
- Fix option: read them on a background thread before the links start (links wait for it). See question.

## G - every photo / video / voice / file comes by itself, 8 MB trips (USER PICK 16 Nov) - PLAN v2 (checked with code)
v1 was checked line by line against app `5bad575` + backend `6db45b6`. What held, what broke, what changed:

| # | v1 said | Code says | v2 |
|---|---|---|---|
| 1 | auto get_file cards | both paths exist: server `AdminChatRequestFile` (admin_chat_221.go l.870), offline `svc.make` (chat_answer_reader l.1046). 314 `already_asked` only checks get_chat (`openRequest314` l.258) | OK, no backend change for asking |
| 2 | one file at a time | a file is only "done" when ALL parts reach the admin; via trips that can take hours, so one-at-a-time blocks every other file | window: at most **2 files / 16 MB** not yet here at once; the next is asked when one is here / refused |
| 3 | "Ready" per file | viewer `_ready310` (l.110) also says ready on `state=='done'`, and `done` = first receipt (`_recount` l.703) = same bug as A | A covers files too: here only when `of>0 && parts>=of` |
| 4 | file_gone shows | rows have no refusal field (`ChatRequestRow221`, `AdminActionRow`); server row neither, but `admin_action_receipts.result` has it (store.go l.1356) | new `refused` field: app row from receipts, server list adds `refused` (backend, small) |
| 5 | carriers 8 MB trips | takePart l.450 + `_sendMissing` l.648 block files for carriers; acks already delete delivered parts (l.547) and reach the source in `have` frames, so the source frees its parts and sends the next = the trip loop works once the block goes | OK |
| 6 | 8 MB per trip | `perMeeting = 40` parts per have exchange (every 60 s, `haveEvery`) = about 1.9 MB/min per carrier | keep 40 (the 16 Nov crash was too many frames); one 8 MB load takes about 4-5 min of contact; told in the card as "loading" |
| 7 | cap 8 MB | `capOthers` refuses part by part when full (log `ADMIN_ANSWER_CAP_FULL` each time) and the source keeps sending | `have` frame tells `room` (bytes left for files); the source sends file parts only while room > 0 |
| 8 | (missing) | carried parts never expire (`SqlAnswerPartStore`, no age delete): a lost / closed answer fills the 8 MB for ever and stops all later trips | others' file parts go after **3 days**; closed / stopped answers get a signed stop (ack `all:true`, existing format) |
| 9 | (missing) | `buildFile` (builder l.473) loads the whole file + all signed parts in memory (50 MB file = about 120 MB) | build + store in slices of 100 parts (same parts, same ids) |
| 10 | (missing) | `_ackMine` reads all parts of the answer and signs on EVERY part (l.468); 2000 parts = 2000 full reads | ack at most once per 2 s per answer + on the last part (send rule "every 20 / all" unchanged) |
| 11 | (missing) | offline saved list keeps 200 rows (`_remember` l.840); a chat with many files would push old Get chat rows out | get_file rows don't count against the 200 Get chat rows; kept while their chat request is kept |
| 12 | (missing) | offline max 50 pending cards per admin (`maxPendingPerAdmin`) | the queue uses at most 3 pending cards, so the Owner can still make actions |
| 13 | Stop files cancels | `cancel` works only while pending (service l.1278); a get_file is `done` right after the receipt | Stop files = stop the queue + close the files not here yet (`close`) + signed stop to carriers |
| 14 | "from ayesha's phone" | rows know the phone that built it (`fromName`); the carrier is only known in `takePart(from:)` | "from kiran's phone"; "via ayesha" only when a carrier brought the last parts (kept in memory) |
| 15 | runs with the screen closed | notifier + carrier live in `AppDependencies` (main engine); the background engine only runs after a reboot until the app opens | same as today's live card: works while LinkUp's engine runs; after swipe-away NOT VERIFIED |
| 16 | web | web viewer has its own Get buttons (views.js l.2871, l.2956) | web: no button, shows the same states (the Owner's phone asks) |

### G plan v2
- G1 File queue (new `file_queue_317.dart`, admin phone, Owner only, driven by the same `changes` + 5 s timer as the
  live card): when a Get chat answer is complete, read its attachments ONCE and save a small plan per request
  (file id, name, type, size, gone). One entry per file id (fixes D duplicates). Newest message first.
  - skip `file_gone` (shows gone), skip size > 50 MB (shows "Too big", never asked);
  - ask with `getFile` (server or offline, same as the button today), window 2 files / 16 MB, max 3 pending cards;
  - next file when one is here or refused; a refused file is never asked again.
- G2 Carriers take file parts: remove the 2 file rules (l.450, l.648); file parts may use **6 of the 8 MB**
  (text always has 2 MB); `room` in the have frame; text parts are sent before file parts (send + upload);
  others' file parts deleted after 3 days; signed stop on close / Stop files.
- G3 Source phone: `buildFile` in slices of 100 parts; one file built at a time (cards wait in turn).
- G4 Admin phone: ack throttle (once per 2 s per answer + last part).
- G5 Rows: `refused` code on app + server rows (backend: read `admin_action_receipts.result`); A's Ready rule
  for files; saved list rule (#11).
- G6 Viewer (app + web): no Get button; Next in line / Coming x / y (bar) / shows + tap opens / Not on the
  phone any more / Too big (over 50 MB). Voice notes the same (play when here).
- G7 Phone card (mock `mocks/n317/trips_mock.html`): the card goes on after "Chat ready":
  - new steps sent from Dart `_args`: `files`, `files_wait` (no new parts 2 min), `files_done`; title / text / short
    "3/13"; trips = 8 MB pieces of the plan's total size (max 7 segments, Android 16 `ProgressStyle` segment limit
    NOT VERIFIED); tracker = new photo icon `ic_req317_file`; blue / orange / green;
  - Android 15 and older: bar by MB, sliding line while waiting;
  - ongoing while files come; waiting more than 30 min -> can be swiped away (still updates);
  - buttons Open chat + Stop files; ONE buzz only (Chat ready); files done = silent; goes when opened after done;
  - keep rule in `_refreshOnce`: a Ready card stays while its files come, even after the chat was opened.
- G8 Admin card: `LiveCard307` gets a files strip (thumbs, "3 of 13 files", MB, from / via, Coming / Waiting /
  All here, trip bar, gone line); `RequestStep307.live` + `liveOrder` keep a Ready request live while files come;
  request row second chip "Files 3/13"; viewer top strip ("All files here" fades after 3 s); web the same.
- Limits (tell user): middle phones use more battery and storage (max 6 MB of files each); a trip loads at about
  1.9 MB per minute of contact; the queue moves only while the Owner's LinkUp runs.

## Other log lines (no fix)
- C71: server off the whole hour -> 136 `WS_CLOSED`, 135 `HTTP_REQUEST_FAILED` (admin pages retry 9 server calls). Expected.
- ayesha: 18x `STORE_OP_SLOW op=outbox_read 1.5-3.1 s` = the database busy with the big part reads -> fixed by 316 F1.
- C71: one `LegacyMessageQueue ... dead thread` warning from a plugin at open; harmless.

## Tests
- A: done + 0 parts -> Coming (not Ready); done + parts < of -> Coming; parts = of -> Ready; empty / cancelled
  unchanged; no Ready buzz before the parts (notifier test); viewer text.
- B: `llev:v1` create / add lines render as a system line in the app and web viewers; the name shows on the next bubble.
- C: deleted look in light + dark (golden check), web.
- D: refused file_gone -> "Not on the phone any more", no button; duplicate file_id shown once.
- G: plan saved once per request, one per file id, newest first; gone / too big never asked; window 2 files /
  16 MB, max 3 pending; refused never again; carrier takes file parts up to 6 MB, text keeps 2 MB, `room` stops
  sending, delete after ack, 3-day expiry, stop on close; text before files; buildFile slices (same bytes as
  before); ack throttle; `refused` on rows (app + server); saved list keeps 200 Get chat rows; viewer + web
  states; card steps files / files_wait / files_done (one buzz only); Kotlin plan() + segments tests.
- E: hint gone after 4 s; no circle under the controls (300 dp to 480 dp wide).
- Regression: every old viewer / conversations / notifier test (307, 310, 313, 314 incl. locks on
  `chat_request_notifier_307_test`), full suite, Kotlin tests, analyze no new issues, web tests.

## As built (3.0.108+179, with 315 + 316, on app `5bad575` / backend `6db45b6`)
- User said "Build" after plan v2 + trips mock: taken as yes to "from kiran's phone · via ayesha", 2 files / 16 MB
  at a time, middle phones carry at most 6 MB of file parts, 3-day expiry.
- A-G as planned (G1-G8). B: `llev:v1:` lines = centred grey chip (app + web). C: 55 % bubble + red
  "Deleted 11:41" (date only on another day; web same rule). D: one row per file id; `refused:file_gone`
  and 60-day `file_gone` keep their own words. E: `link_paths_full_screen.dart` only (map file sha-locked).
  F: `startSeenLoad317` thread `linkup-seen-load`, log `SEEN_MESH_LOADED ms=` tag `LocalLinkBoot`.
- Backend: `refused` on request rows (from `admin_action_receipts.result`); web viewer states, no Get button.
- Checked 16 Nov: the double "Next in line" seen when a test passed `view:` = the second file (g1) without its
  refusal row (`_loadFiles310` runs only when no `view`). No app code passes `view:` (notification
  `local_link_app.dart` l.748 + Conversations l.186 pass service + request), so the real app is not hit.
