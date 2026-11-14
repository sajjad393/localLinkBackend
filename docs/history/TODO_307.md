# TODO 307 - Get chat fixes + Conversations redesign (bold cards) + live request card - app + web

**Status:** PLAN READY, mock v2 APPROVED ("Good", 15 Nov). FREEZE ON - nothing is built until you say "Build".

| | |
|---|---|
| Mock | `mocks/conversations_307_v2.html` (generator `mocks/m307/mk_mock_307v2.py`) |
| App | **3.0.101+172**, base = 304 pushed `228ed49` |
| Backend | **bundle 307**, base = 300 pushed `26df549` (deploy first) |
| Deliverables | 1 backend zip + 1 app zip, 1 commit each (`v3 backend 307: ...`, `v3.0.101+172: 307 ...`), one PowerShell block, backend first |
| Rule | "If anything works don't touch it" - only the files listed here change |

Code was read at `228ed49` / `26df549` (line numbers can move a few lines).

---------------------------------------------------------------------------------------------------

## 0. What you get (simple)

1. **Get chat finds the chat** even when it was written from a friend's old phone (A9 - the real 13 Nov bug), and **stops getting stuck.** "No copy" answers reach you even through a relay phone. Requests close by themselves. The web shows offline requests too.
2. **New Conversations look** (app + web), built around the Uber-style live card:
   - a big title and a small server pill;
   - big rounded cards and progress rings;
   - a "Ready to read" row.
3. **Live card on the phone** (lock screen + notifications):
   - Android 16 shows it as a Live Update with the moving chat marker and a "12/40" chip next to the clock;
   - older Android shows a plain progress bar.
   - Several requests are grouped. Ready gives one buzz and an "Open chat" button.
4. **Animations** in the app and on the web (the lock screen can't animate - Android draws it).

---------------------------------------------------------------------------------------------------

## Part A - Get chat fixes (from the 13 Nov logs + deep check)

| # | Problem today (code) | Fix |
|---|---|---|
| A1 | The "no copy here" note goes only to the phone that brought the card. `admin_action_service.dart` l.892 sends `_sendSeen(e.extra['from'], ...)`, and `from` is saved at l.530. If that phone was a relay (ZTE `NOT_MINE`), the note dies. | 1) Also send the note to the card maker's phone (from the signed card). 2) A relay that gets a seen note for someone else passes it on once (keep the 20-note cap of `_onSeen` ~l.756; no loops: remember forwarded note ids, max 200). 3) `syncWithServer` (l.1201) also uploads seen notes. |
| A2 | Requests made by the server never close as "No messages". | **Backend:** when every target phone has sent "empty", the request becomes `cancelled` + `empty: true`, and `AdminChatRequestList` (`admin_chat_221.go` l.925) returns `empty`. **App:** `ChatRequestRow221.fromServer` (`chat_answer_reader.dart` ~l.440) reads `empty` (today it never does). |
| A3 | "File not on this phone" is not counted as no copy. `chat_answer_builder.dart` l.435 throws `no_file`; `admin_action_doers.dart` l.82 lets another phone answer, but no "empty" note is sent. | `no_file` sends the same "empty" seen note as `no_copy_here`. |
| A4 | Offline requests reach the server without chat details, so the web can't list them or fetch their parts. | **App:** offline cards upload `chat` (kind + users/devices or group id), `words` and `label` in the card extra. **Backend:** `storeIncomingActionCap` (`admin_actions_220.go` ~l.525) saves them. |
| A5 | Closed requests are never uploaded: `syncWithServer` sends pending cards only (l.1213 / l.1220). | Also upload the final state (done / cancelled / empty) once per request. |
| A6 | Closed rows still say "since ..." and show the link-path note (`_requestRow` l.242). | Closed rows show only the finished date (the new Finished list, B6). |
| A7 | The server pill is only checked at load (`_srvOnline`, l.38 / l.60-72). | Re-check when `viewMode` server state changes and every 10 s while the screen is open. |
| **A9 (main bug)** | **Phones that HAVE the chat answer "No messages".** The user confirmed kiran & ayesha have messages, yet on 13 Nov BOTH kiran's phone (`…9654237e`, seen note at 18:58:54.414 on RMX5303) and ayesha's phone (RMX3938, `CHAT_REQ_EMPTY` 18:58:54 + 19:26:45) answered "empty". The reader `SqlChatRows221.rows()` (`hidden_store_221.dart` l.428-442) looks only for messages where sender AND receiver are in `chat.devices` - just each person's phone id **today**. Since bundle 237 ("one chat per friend") a chat also keeps messages with the friend's **old phone ids** (after a reinstall or new phone). The chat screen, Clear chat and the unread count all use `phonesOfPerson237()` (`local_store.dart` l.2379 + `phonesWhere237` l.2416), but Get chat does not. kiran had duplicate / old accounts (fixed on the server in 279), so his old messages are most likely stored under an old id. NOT VERIFIED on the phone DB, but it is the only code path that gives 0 rows for a chat that is on screen. | In `rows()`, a direct chat widens each id in `chat.devices` with `phonesOfPerson237(id)` (current + old phones of the same person; an id that is now someone else's current phone is skipped by the existing `_isOtherPersonsCurrent237`). It adds the phones of `chat.users` from the contacts / directory and this phone's own past ids (`PersonPhones237.pastOf(selfUser)`). The query then becomes `sender IN all AND recipient IN all AND sender<>recipient`, exactly like the chat screen. Targets do not change (old phones never become targets, so requests can still finish). New log: `CHAT_REQ_EMPTY id= devs=2 widened=N rows=0` so a future "empty" shows why. |
| A8 | Builds below 132 can't answer, and phones without the server key hold the card. | No code fix. The row says "Old LinkUp on this phone" (existing `oldApp` text), so it never looks stuck forever. |

**A9 note:** with A9 the 611d9a case would answer from both phones, so A1/A2 then only matter for chats that really are on no phone.

**Backend AdminActionsSync (l.838):** accepts `seen` notes, checks each signature with that phone's key, saves them per request, and drops bad or duplicate notes quietly.

---------------------------------------------------------------------------------------------------

## Part B - App: new look (mock sections 1 + 2)

### B0. Design rules

The colours come from `admin_ui216.dart` (A216 light + dark) only. Font is Roboto. Wording: no "Wi-Fi Direct" / "Bluetooth" anywhere on these screens.

| Thing | Light | Dark |
|---|---|---|
| Screen bg | `#F5F6FA` | `#0D1117` |
| Card | `#FFFFFF`, shadow 0/14/34 at 9 % | `#161B22` + 1 px `#232B36` edge, no soft shadow |
| Ink / grey | `#101828` / `#667085` | `#E6EDF3` / `#8B949E` |
| Blue (number, fill) | `#1E5BFF` | `#4C8DFF` (text `#7AA7FF`) |
| Green (Ready) | `#12805C` on `#E7F7EE`, dot `#12B76A` | `#4ADE80` on `#12351F` |
| Orange (Sent / Reached) | `#B54708` on `#FFF4E5` | `#FBBF24` on `#3A2A10` |
| Dashes / ring track | `#D0D5DD` / `#EEF0F4` | `#3A424D` / `#21262D` |
| Right bubble | `#1E5BFF` | `#2F6BFF` |

- **Corners:**
  - live card 24;
  - request card / grouped list / tiles 20;
  - sheet 28 (top);
  - segmented 14;
  - buttons 18 (big) / 16 (capsule).
- **Type:**
  - title 30/700;
  - section 17/700;
  - card big number 22/700 (21 in the app carousel);
  - names 15-16/700;
  - body 13.5-14.5;
  - small 12.
- **Avatars:** the existing hash colours (`h*31+u` mod 8). Two faces side by side, the second overlapping by 26 % with a 2.5 px ring in the card colour. A group gets a rounded-square group icon.

### B1. New shared widgets (new file `lib/features/admin/presentation/widgets/conversations_kit307.dart`)

| Widget | What |
|---|---|
| `RequestStep307` (pure Dart, `data/actions/request_step_307.dart`) | One place that turns a request into: `step` (sent / reached / coming / ready / empty / cancelled), `progress` 0-100, `ring` text (`…`, `1/2`, `30%`, check), big text + highlight, sub line, chip text, short text (`12/40`). Built from `ChatRequestRow221` + `AdminActionRow.done/need` + `AnswerProgress(have, of)`. Used by the app, the notifier and the tests. |
| `LiveCard307` | The Uber card. **Top row:** blue square icon + "Get chat" on the left; two faces (40) on the right. **Middle row:** the big step text with its number in blue ("**12 of 40** parts"), and the sub line under it; on the right, the names and the range (Full chat / Last 7 days ...). **Bottom:** `RequestTrack307`. **Ready:** 2 px green edge. **No messages:** greyed out. |
| `RequestTrack307` | A dashed line with a solid blue fill up to `progress`. Stops sit at 33 % (Reached), 66 % (Answer) and 100 % (Ready). Passed stops are small solid dots. The next stop is a blue dot with a soft halo and a pulse ring. The end turns into a green circle with a check on Ready. The marker is a white capsule (dark: `#2A313C`) with the chat icon, riding at `progress`. Start = 4 %, Reached = 33 %, parts = 66 % to 100 %. |
| `ProgressRing307` | 48 px ring. The fill is orange for Sent/Reached and blue for parts; the centre shows the ring text. Ready = solid green circle with a white check. No messages = grey. |
| `AvatarPair307` | Two faces side by side (or the group icon). |
| `Segmented307` | Two-part switch with a sliding white knob. Each label is followed by a small count badge (selected = blue badge). |
| `SectionHead307` | Bold section title + grey count badge + optional blue link on the right. |
| `BigButton307` / `Fab307` | Blue gradient `#4C80FF -> #1E5BFF`, shadow. FAB = 56 high capsule "+ New request". |

### B2. Header (replaces `LinkBanner221` l.447)

- **Row 1:** a round back button (38, card colour) on the left; a round crown button on the right. It keeps `AdminCrown.mark(15, halo: null)` (locked by `crown_tip_290_test`).
- **Row 2:** "Conversations" 30/700 + the server pill:
  - green "Server connected" with a gently breathing dot;
  - or grey "No server".
- The pill keeps key `chats221_online_bar` / `chats221_offline_bar`. Tapping it opens the old one-line explanation as a small tip (no Wi-Fi Direct words).
- `chats221_owner` key stays on the screen root.

### B3. Switch

`Segmented307`: "Chats [12]" / "Requests [3]". It keeps the keys `tabs221_0` / `tabs221_1`.

### B4. Requests tab - "Live" carousel

- **Section head:** "Live [3]" + "See all". See all scrolls down to "In progress".
- **Cards:** one `LiveCard307` per active request, full width, swiped sideways (`PageView`, 12 px gap), with dots under it.
- **Order:** Ready first, then the newest.
- **Auto-advance:** every 5 s only when there are 2+ cards and the user is not touching. It stops for 10 s after a touch.
- **Tap a card:** Ready opens the viewer. Otherwise a small sheet shows "Phones that haven't answered yet" (existing text) and "Cancel request".
- **No active requests:** no carousel. The existing empty text `requests221_empty` shows under "In progress".

### B5. Requests tab - "In progress"

- One card per active request (`requests221_row_<id>`), with 10 px between cards.
- **Left:** `AvatarPair307` (42).
- **Middle, line 1:** names 16/700.
- **Middle, line 2:** the sub line (grey).
- **Middle, line 3:** the chip ("Sent" / "1 of 2 phones" / "12 of 40" / "Ready") + the range (grey) + "Cancel" (red text, `requests221_cancel_<id>`, only when allowed today).
- **Right:** `ProgressRing307`.
- A thin bar with `parts221_bar` stays inside the card (under line 3, 3 px, only while parts arrive). The row text keeps "40 of 80 parts" (test).
- The Cancel confirm stays as today (`chat_request_cancel_yes`).

### B6. Requests tab - "Finished"

- **Layout:** one grouped card holding all the rows, with thin dividers.
- **Each row:** two faces (36) + names + a small tag ("✓ Ready" green / "No messages" grey / "Cancelled" grey) + "26 parts · 14 Nov" + a chevron.
- **No "since", no path note (A6).** Tapping Ready opens the viewer.

### B7. Chats tab

- **Search:** a white field, 44 high, r14, "Search people or groups".
- **"Ready to read [N]":** a sideways row of tiles (142 wide). Each tile has two faces, the names, "128 messages · 14:20" and a green "Open" capsule.
  - The row shows only chats with a finished answer whose copy is still kept.
  - It is hidden when there are none.
- **"All chats [12]":** one grouped card (`chats221_list`). Each row (`chats221_row_<id>`) has two faces (42), the names, "56 messages · Yesterday" and, on the right, one of:
  - blue capsule "Get chat" (`chats221_ask_<id>`), or
  - a chip while a request is running ("1 of 2" / "Sent"), or
  - "Ready".
- Group rows: group icon + "Group · 5 members" (test text kept).
- Empty: `chats221_empty` as today.
- **FAB** "+ New request" (`chats221_new_request`) on both tabs.

### B8. New request sheet (`admin_chat_request_sheet.dart`)

- **Top:** grabber, then "New request" 24/700.
- **Note:** the existing note text, kept word for word: "Their phones send these messages (also ones they deleted). They see nothing." It is locked by a test.
- **Switch:** `Segmented307` small: "Private chat" / "Group" (`request221_group`).
- **People:** two big tiles side by side with a round swap button in the middle.
  - Before a person is picked, the tiles show "First person" / "Second person" (keys `request221_first` / `request221_second`; the text is locked by a test).
  - After picking: face (48), name, @username.
  - The picker list is unchanged (`pick221_<id>`, `pick254_me_tag`, "Me (sajjad)", "@noshad · old account").
- **Messages:** a 2x2 grid of tiles, each with icon, title and small line:
  - Full chat / Everything;
  - 7 days / Last week;
  - 30 days / Last month;
  - Dates / Pick a range.
  - Selected tile: blue edge, blue tint, small check.
  - Keys `request221_range_<full|7|30|dates>`.
- **Warnings:** the same-phone note `request254_same_phone` and "You already have this chat" stay, shown above the button.
- **Button:** big "Get chat" (`request221_send`); "Sending…" while busy.
- **After send:** the sheet slides down (300 ms), and the new live card drops in at the front of the carousel (B4, E-table).

### B9. Answer viewer (`admin_chat_viewer_screen.dart`)

- **Header:** round back button, two faces (30), names 16/700, then "Full chat · ayesha's phone" (one line, ellipsis), and a round ⋮ menu (`viewer221_menu`: Download / Copy text / Delete copy / Delete completely - unchanged).
- **Progress card** under the header (`viewer221_parts` key moves here):
  - "**28 of 40** parts" + "the rest is on the way" (existing `ofPartsTheRestIsOn` text kept: "40 of 80 parts · the rest is on the way");
  - a small `ProgressRing307` on the right;
  - when all parts are in: "**Ready** to read" + green ring + green edge, then it shrinks away after 3 s.
- **Bubbles:**
  - radius 20, the tail corner 6;
  - left bubbles in the card colour, right bubbles blue with white text;
  - sender name in their hash colour.
- **Date chips:** small and round.
- **"Deleted by X" tag:** red on light red (white pill with red text on blue bubbles).
- **Kept:** "Outside the dates (phone clock)", "File removed after 60 days · thumbnail only", "Get file" (`viewer221_getfile_<id>`), `_nameFor255`, `photoIds: _photoIds255` (tests).
- **Route words removed:** the Server / Wi-Fi Direct / Mesh / Local Wi-Fi labels (`AdminChatViewerScreenRoutes`, l.20-28) are no longer shown. The class stays so nothing else breaks.

### B10. States

| State | Look |
|---|---|
| Loading | Shimmer: one big card (150 high, r24) + 3 row cards with ring placeholders. |
| No server | Grey pill. Requests still work offline (as today). |
| Not owner | As today (owner only). |
| Error loading | The existing message in a card + "Try again". |

---------------------------------------------------------------------------------------------------

## Part C - Phone card (lock screen + notifications) (mock sections 3 + 4)

### C0. The steps (same words in the app, phone and web)

| Step | Big text (highlight in **bold**) | Sub line | Chip | Ring | Status chip |
|---|---|---|---|---|---|
| Sent | **Sent** to 2 phones | Waiting for their phones | Sent | … | Sent |
| Reached | **1 of 2** phones | Looking for the chat | 1 of 2 phones | 1/2 | 1/2 |
| Answer coming | **12 of 40** parts | Answer coming from ayesha's phone | 12 of 40 | 30% | 12/40 |
| Ready | **Ready** to read | Tap to open · 40 parts | Ready | ✓ | Ready |
| No messages | No messages (grey) | No phone has this chat | No messages | 0 | – |

The data already exists; no new tracking:
- `AdminActionRow.done/need` (`rows()` l.235);
- the seen lists `extra['empty']` / `extra['wait_trust']`;
- `AnswerProgress (have, of)` (`chat_answer_carrier.dart` l.298, `progressOf` l.723);
- the server `parts/of`.

### C1. Native: new `android/.../ChatRequestNotifier307.kt`

- **Channel methods:** new `showChatRequest` / `cancelChatRequest` / `cancelAllChatRequests` on the existing `locallink/notifications` channel. Added in BOTH `MainActivity.kt` (~l.713) and `BackgroundChannels.kt` (~l.80), so it also works with the app closed.
- **Words:** Dart sends the final words (already in English or Urdu). Kotlin only draws them, so `Words304.kt` is not touched.
- **Android 16+** (`SDK_INT >= 36`), framework `Notification.Builder` (no new library):
  - `Notification.ProgressStyle` with:
    - progress 0-100;
    - points at 33 / 66 / 100 (blue; Ready point green);
    - one segment;
    - tracker icon = chat bubble.
  - `setOngoing(true)` and request promoted ongoing, so it becomes a Live Update.
  - `setShortCriticalText("12/40")` for the chip next to the clock.
  - Title = big text, text = sub line, sub text = "kiran & ayesha", large icon = two faces (the existing `letterCircle` drawing).
- **Android 8-15:** `NotificationCompat` with the same title/text and `setProgress(100, p, false)`. Indeterminate while Sent.
- **One notification per request** (id from the request id), all in group `linkup_chat_requests`.
  - The summary says "3 chat requests · 1 ready" and lists one line per request. The summary itself is never promoted (Android rule).
- **Channel:** new `locallink_chat_requests_v1` "LinkUp chat requests", importance DEFAULT. Updates are silent (`setOnlyAlertOnce(true)`).
- **Ready:**
  - posted once more with sound + one short buzz (the existing message tone);
  - not ongoing any more;
  - button "Open chat", and tapping it opens the viewer (`open_chat_request` extra -> existing deep-link path);
  - it moves to the top of the group (newest `when`).
- **No messages:** grey text, not ongoing, removed after 10 s (`setTimeoutAfter(10000)`).
- **Removed when:** the request is cancelled, you log out, or you leave the owner role (`cancelAllChatRequests`).

### C2. Manifest

`+ <uses-permission android:name="android.permission.POST_PROMOTED_NOTIFICATIONS" />`
- It is a normal permission: no pop-up (rule 263 kept).
- Permission count 26 -> 27.
- If you turn Live Updates off in Settings, the same card shows as a normal notification.

### C3. Dart: new `lib/features/admin/data/actions/chat_request_notifier_307.dart`

- Listens to `AdminActionService` (`_publish` / `book`), `ChatAnswerCarrier.progress` (ValueNotifier) and the server request list.
- Only on the phone that made the request (owner, `mine`).
- Sends an update only when the step or the count changes, at most 1 per second per request.
- The Ready buzz is sent once per request (remembered in settings, key `chat_req_ready_buzzed_307`, max 100 ids).
- Wired in `lib/app/app_dependencies.dart` next to `ChatRequests221` (~l.1084).

---------------------------------------------------------------------------------------------------

## Part D - Web Conversations (mock section 5)

**Files:** backend `internal/httpapi/adminweb/views.js` (conversations l.2007, `liveRequest` l.2220, detail l.2570, `loadRequests` l.2585) and `styles.css` (new `/* 307 */` block after the 216 block, l.1026+). English only (the PC dashboard is not in Urdu).

| # | What |
|---|---|
| D1 | **Top bar:** "Conversations" 28/700 + "Chats the phones keep · Owner only" on the left. On the right: server pill, dark-mode button, blue "+ New request" button (opens the existing sheet), and the owner avatar with its gold ring. |
| D2 | **"Live requests [3]" + "View all":** up to 3 `LiveCard` cards in a row (same layout as the app; Ready cards get a green edge). View all opens the Requests list filtered to active. |
| D3 | **Two columns:** left 400 px list card, right viewer card, both r20. |
| D4 | **Left list:** Chats / Requests switch, search, "In progress" (request cards with rings + Cancel) and "Finished" (grouped rows with tags; the selected row is tinted blue). |
| D5 | **Right viewer:** header with faces, names, "Full chat · from ibrahim's phone · 26 parts", a Ready tag, Download and Delete copy. Then date chips and bubbles (blue right bubbles) with the "Deleted by" tag. |
| D6 | The Conversations page no longer shows the `ROUTE221` words (l.2212). The constant stays. |
| D7 | Every string and class locked by `admin_web_221_test.go` stays: "Request from phones", "Send request", "Choose dates", "Reading is recorded", `.info221 .viewer221 .del221p .sheet221 .prog221 .del221go`, `mountView('conversations', { quiet: true, refresh: true })`, and the rest. |
| D8 | The quiet refresh (every few seconds, as today) animates only what changed: the marker slides, numbers count up, Ready pops. Nothing jumps or reloads the page. |

---------------------------------------------------------------------------------------------------

## Part E - Animations (app + web; the lock screen only changes the bar, the words and the buzz)

| What | How | Time |
|---|---|---|
| Rows / cards come in | slide up 14 px + fade + scale .98 -> 1, one by one | 500 ms, +70 ms per row (max 8) |
| Switch knob | slides | 350 ms ease-out |
| Marker on the track | glides to the new spot + gentle 1.5 px bob | 700 ms |
| Next stop | halo + pulse ring | 1.8 s loop |
| Numbers | count up (12 -> 13 -> ...) | per update |
| Chip / ring colour | colour fade | 350 ms |
| Ready | card pops (scale 1.035), the end dot turns green with the check drawn in, green edge | 550 ms |
| No messages | card goes grey, then (phone card) fades away | 500 ms |
| Carousel | page slide; the dot stretches into a pill | 550 ms |
| Send in sheet | the button presses (.96); the sheet slides down; the new card drops in from above with a small spring | 200 / 300 / 600 ms |
| Viewer | messages fade in by batches of 3; the progress card shrinks away when complete | 400 ms each |
| Loading | shimmer | 1.4 s loop |
| Server pill dot | breathing glow | 2 s loop |

- When "Remove animations" (Android) / `prefers-reduced-motion` (web) is on, everything shows the end state with no movement.
- Loops run only while visible (`TickerMode`) and never in widget tests (no `repeat()` under `pumpAndSettle`).

---------------------------------------------------------------------------------------------------

## Part F - Words (new `.arb` keys; English + Urdu, LTR as in 304)

Rules:
- Added by hand to `app_en.arb` + `app_ur.arb` (`ur_order.json` untouched, never regenerated).
- Highlighted parts use the 304 `L.spans` helper, with FSI…PDI around placeholders.
- Existing keys are reused: `serverConnected`, `getChat`, `ready`, `ofParts`, `noMessages`, `open`, `cancel`, `live`, `fullChat`, `t7Days`, `t30Days`, `dates`, `newRequest`, `ofPartsTheRestIsOn`.

| Key | English | Urdu (native review like 304) |
|---|---|---|
| `inProgress307` | In progress | جاری ہے |
| `finished307` | Finished | مکمل |
| `seeAll307` | See all | سب دیکھیں |
| `readyToReadSection307` | Ready to read | پڑھنے کے لیے تیار |
| `allChats307` | All chats | تمام چیٹس |
| `noServer307` | No server | سرور نہیں |
| `stepSent307` | {hl} to {n} phones (hl = Sent) | {n} فونز کو {hl} (hl = بھیج دی گئی) |
| `stepPhones307` | {hl} phones (hl = "1 of 2") | {hl} فونز (hl = "2 میں سے 1") |
| `stepParts307` | {hl} parts (hl = "12 of 40") | {hl} حصے (hl = "40 میں سے 12") |
| `xOfY307` | {a} of {b} | {b} میں سے {a} |
| `stepReady307` | {hl} to read (hl = Ready) | پڑھنے کے لیے {hl} (hl = تیار) |
| `waitingForTheirPhones307` | Waiting for their phones | ان کے فونز کا انتظار ہے |
| `lookingForTheChat307` | Looking for the chat | چیٹ تلاش ہو رہی ہے |
| `answerComingFrom307` | Answer coming from {name}'s phone | {name} کے فون سے جواب آ رہا ہے |
| `tapToOpenParts307` | Tap to open · {n} parts | کھولنے کے لیے ٹیپ کریں · {n} حصے |
| `noPhoneHasThisChat307` | No phone has this chat | کسی فون میں یہ چیٹ نہیں ہے |
| `chatReady307` | Chat ready | چیٹ تیار ہے |
| `openChat307` | Open chat | چیٹ کھولیں |
| `chatRequestsSummary307` | {n} chat requests · {r} ready | {n} چیٹ درخواستیں · {r} تیار |
| `chatRequestsChannel307` | LinkUp chat requests | LinkUp چیٹ درخواستیں |
| `everything307` | Everything | سب کچھ |
| `lastWeek307` | Last week | پچھلا ہفتہ |
| `lastMonth307` | Last month | پچھلا مہینہ |
| `pickARange307` | Pick a range | تاریخیں چنیں |
| `messagesAt307` | {n} messages · {when} | {n} پیغامات · {when} |

---------------------------------------------------------------------------------------------------

## Part G - Tests

### New

**`test/conversations_307_test.dart`:**
- A9 (unit, sqflite ffi): messages stored under kiran's OLD phone id + ayesha's id -> `rows()` finds them all; a phone id that is now another person's current phone is never added; group chats unchanged; the empty case logs `widened=`.
- A1-A7 (unit):
  - a seen note passes a relay and reaches the maker;
  - `no_file` sends "empty";
  - the server `empty` field is read;
  - the final state is uploaded once;
  - a closed row has no "since";
  - the pill flips within 10 s when the server drops.
- `RequestStep307`: every step gives the right progress, ring text, chip and short text (the table in C0), including 40/40 -> Ready and the group case.
- Widgets, light + dark:
  - header pill;
  - carousel + dots, auto-advance stops on touch;
  - In progress card (chip + ring + Cancel);
  - Finished tags;
  - Ready-to-read row hidden when empty;
  - sheet tiles + range grid + send (the card appears at the front);
  - viewer progress card -> Ready.
- No "Wi-Fi Direct" text on the 3 screens.
- Urdu: every new key exists in `app_ur.arb`; the Urdu screen renders without overflow (`loadRoboto216` + Naskh).

**`test/chat_request_notifier_307_test.dart`:**
- the channel call arguments per step;
- at most 1 update per second;
- Ready buzzes once only;
- not on non-owner phones;
- cancel-all on logout.

**`android/app/src/test/kotlin/.../ChatRequest307Test.kt`:**
- the pure mapper (step -> progress / points / ongoing / timeout);
- the API-36 path is chosen on 36+ and the progress-bar path below;
- Ready is not ongoing;
- one group key.

The Android drawing itself is not unit-testable in the sandbox; it is checked on the phone.

**`internal/httpapi/admin_chat_307_test.go`:**
- seen notes are saved and bad signatures refused;
- all-empty -> cancelled + `empty`;
- offline card chat details are saved and listed;
- the closed-state upload works;
- the page has the live row, track and ring classes;
- the `admin_web_221` strings are still there.

### Listed old-test changes (only these)

| File | Line | Change |
|---|---|---|
| `test/bundle221b_test.dart` | l.113 | `'Server connected · requests go by the server'` -> `'Server connected'` |
| | l.166 | `'Requests 2'` -> the tab label `'Requests'` + badge `'2'` |
| | l.180 | `'No server · requests go by Wi-Fi Direct / mesh'` -> `'No server'` |
| | l.215-218 | route words `'Wi-Fi Direct'` x2 / `'Server'` / `'Mesh'` / `'Local Wi-Fi'` -> `findsNothing` |
| `test/bundle210f_screen_test.dart` | l.43 | permission count 26 -> 27 |

Everything else stays exactly as it is. Kept on purpose:
- keys `chats221_*`, `requests221_*`, `request221_*`, `pick221_*`, `viewer221_*`, `parts221_bar`, `tabs221_*`;
- the texts "First person", "Full chat", "They see nothing", "40 of 80 parts", "Deleted by user · 5 Nov 14:02".

### Regression (every build)

- The full app suite (3041 today + new), in chunks (`run_chunks_tc.sh`), with grep `[E]` = 0.
- `flutter analyze` with no new issues.
- Kotlin tests (kt kit, `-language-version 2.2` + all `.kt` + `RStub.kt`).
- `go vet ./...` + `go test ./...`.
- Report every command with its exit code. Never weaken or delete a test.

---------------------------------------------------------------------------------------------------

## Part H - Build order (after "Build")

| Phase | Work | Check |
|---|---|---|
| 0 | Re-create the toolchain (`setup304.sh` -> `setup307.sh`: clone `228ed49` / `26df549`, Flutter 3.47.7, Naskh). Confirm `flutter.compileSdkVersion >= 36` (needed for ProgressStyle). If it is lower, set `compileSdk 36` in `android/app/build.gradle` (one line, listed). The 36+ code always runs behind `Build.VERSION.SDK_INT >= 36` guards. | toolchain OK |
| 1a | **App first check:** A9 reader fix + test (smallest, highest value). | unit test |
| 1 | **Backend:** A1 sync seen notes, A2 close + `empty`, A4 save chat details, A5 closed state. | go tests |
| 2 | **Backend:** web D1-D8 + CSS. | go tests + page check |
| 3 | Backend zip + APPLY_backend_307.md | — |
| 4 | **App:** A1-A7 fixes. | unit tests |
| 5 | **App:** `RequestStep307` + `conversations_kit307.dart` widgets. | widget tests |
| 6 | **App:** screens B2-B10 (Requests, Chats, sheet, viewer). | widget + golden-free checks |
| 7 | **App:** words F (en + ur). | Urdu test |
| 8 | **App:** native notifier C1-C3 + manifest. | Kotlin + Dart tests |
| 9 | **App:** animations E (reduce-motion respected). | tests stay green |
| 10 | Full regression; version 3.0.101+172 (`bump.py`); app zip + APPLY_app_307.md; update PENDING.md. | all green |

**Files that change:**

App:
- `admin_conversations_screen.dart`
- `admin_chat_request_sheet.dart`
- `admin_chat_viewer_screen.dart`
- `admin_action_service.dart`
- `admin_action_doers.dart`
- `chat_answer_builder.dart`
- `chat_answer_reader.dart`
- `hidden_store_221.dart` (A9, `rows()` only)
- `app_dependencies.dart` (wiring only)
- `MainActivity.kt`
- `BackgroundChannels.kt`
- `AndroidManifest.xml`
- `app_en.arb`, `app_ur.arb`
- the 2 listed tests
- `app_build.dart` / `pubspec.yaml` (version)
- new: `request_step_307.dart`, `chat_request_notifier_307.dart`, `conversations_kit307.dart`, `ChatRequestNotifier307.kt` + tests

Backend:
- `admin_actions_220.go`
- `admin_chat_221.go`
- `adminweb/views.js`
- `adminweb/styles.css`
- new test

**PowerShell `git add`:**
- App: `git add -A` + `git add -f` for `.md`.
- Backend: `git add internal/httpapi` (+ `internal/store` only if touched).

---------------------------------------------------------------------------------------------------

## Part I - Phone checks (after you install)

1. **Online request** (server on): the live card moves Sent -> 1 of 2 -> parts -> Ready. You get one buzz and "Open chat" opens the viewer.
2. **Offline request** (server off): the same, and later it shows on the web with names (A4).
3. **Ask for kiran & ayesha's chat again:** it must come back with their messages (A9), including the old ones.
4. **Ask for a chat nobody has:** it shows "No messages" and goes away; the web closes it too (A1 / A2).
5. **Route it through the ZTE as a relay:** it still closes (A1).
6. **3 requests at once:** they are grouped on the lock screen as "3 chat requests · 1 ready", Ready is on top, and the carousel has 3 cards.
7. **Android 16 phone:** a "12/40" chip next to the clock. **Older phones (ZTE):** a plain bar with the same words.
8. **Server off while the screen is open:** the pill turns grey within 10 s.
9. **Urdu language:** all the new words show in Urdu, left-to-right, with no cut text.
10. **Web, light + dark:** live row, list, viewer, and the quiet refresh animates.
11. **Regression:** calls, messages, Add Contact and the admin map work as before.

---------------------------------------------------------------------------------------------------

## Limits and honest notes

- The lock-screen card is drawn by Android, so it looks close to the app card but not identical, and it cannot animate (only the bar, the words, the chip and the buzz change).
- Live Update needs Android 16 QPR1 or newer, and the user can switch it off per app. Which of your phones run 16 is NOT VERIFIED.
- The group summary is never a Live Update (Android rule). Each request card can be.
- If Live Updates are rejected, the card still shows as a normal ongoing notification. Nothing is lost.
- **Answered (15 Nov):** kiran & ayesha DO have messages, so "No messages" was wrong. That is A9. The exact old phone id is NOT VERIFIED (no phone DB access); the A9 log line will prove it after the build.
