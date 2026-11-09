# TODO 223 - everything from the 2026-11-06 logs, photo and talk (all phases)

Status: **GO AHEAD given 2026-11-06** ("Convert into Todo plan. Start apply fixed
phase wise ... write a test for every single issue"). All phases 1-7 are in 223.
Phase 8 = watch items (log lines only, no behaviour change).
Version: **3.0.63+134**. No phone database change. No server migration.

Mocks: `mock_223_qr_text.png` (section D), `mock_223_step2.png`
(section E), and both together in `mock_223_all.jpg`.

Sources: logs `LinkUp-logs-RMX5303-2026-11-06-0751.txt` and
`LinkUp-logs-Z2453-2026-11-06-0750.txt`, your photo
`IMG20261106072532.jpg` (07:25), your answers on 2026-11-06, and
`PLAN_223_PHASES.md` (the full log reading).

## Phases (order of work; each phase is tested before the next starts)

| Phase | Sections | What |
|---|---|---|
| 1 Server | A | status refused after a clock change |
| 2 Server link | B, C, H, D | join key from 1 phone, WS errors, WS retry stuck 65 s, QR text |
| 3 First start | I, J | permission pop-ups one at a time, password lock off the screen thread |
| 4 No network | K, L, M, N | LAN beacon back-off, skip server calls offline, chat opens at once, phone-only account |
| 5 Share step 2 | E | wlan0 hotspot address, wait + list + Try again |
| 6 Wi-Fi Direct | F | exact TXT ask + name tag `LinkUp-<id8>-<flags>` |
| 7 Logs | O | stamp, WARN->INFO, repeated lines, reasons |
| 8 Watch | P | extra log for `no_shared_key`; no behaviour change |

Rule: if a fix shows another bug in the same code, it is fixed in the same
phase and written in section Q ("found while fixing").

---

## Your choices so far

| # | Question | Your answer |
|---|---|---|
| Scope | What goes in 223 | 1, 2, 4 (+ 8 QR text). Sections E and F below are NEW and need your yes |
| Join key | How a phone with no server trusts the key | From ONE phone if the server's signature checks |
| Clock | What happens after a clock jump | The stuck counter resets itself (no warning screen) |
| 07:37 drop | Did you restart the server | Yes, so nothing to fix |
| QR text | Add the text | Yes: "Scan with the camera app, not the Wi-Fi scanner" |

---

## Overview: why "alex" is not on the server or on the app admin

You made an offline account "alex" on the ZTE. It needs **two** roads,
and both were blocked:

| Road | What should happen | What blocked it | Section |
|---|---|---|---|
| A. To the server | The ZTE packs alex (a "join box"), the Realme carries it to the server | The ZTE never packed it: no join key (needs 2 phones) | **B** |
| B. "Seen nearby" to the server | The Realme tells the server "I see offline alex" in its status update | The server refused every status update (clock) | **A** |
| C. To the app admin (Realme) | The Realme gets alex's profile card nearby | The phones never connected after 07:47 (see below) | **F** + **G** |

Why the phones never connected after 07:47 (road C):
1. **The ZTE's Wi-Fi went OFF at 07:47:10** (`NET_CHANGE wifi=false`), right
   after the clear data. With no Wi-Fi, the router (LAN) road is gone. All
   LAN sends then failed with `ENETUNREACH`.
2. **Clearing data also removed the ZTE's Nearby permission**
   (`perm=false` at 07:46:59, `WIFI_PERMISSION_UNAVAILABLE_LAN_ONLY` at
   07:47:36). It was given again later (`perm=y` at 07:50).
3. **The Wi-Fi Direct name tags never get through on these two phones.**
   In ALL logs (Nov 5 and Nov 6, both phones), every search shows
   `heard=0`. Each phone only receives the short "LocalLink" name (PTR),
   never the tag details (TXT). So "Pair" waits for a tag that never comes
   (`PAIR_AMBIGUOUS count=0`, 3 tries on each phone at 07:48-07:49).
   Before 07:47 the phones only worked because both were on the same router.

---

## A. Server: status refused after a clock change (backend) - issue 1

**What we saw:** 12 of 12 Realme status updates got "presence update rate
limit exceeded". You confirmed the server PC clock was wrong.

**Cause:** the server saves when each counter started. When the clock was
ahead and was then put back, that start time is in the future. The counter
never ends, so the phone is refused forever. The 2-hour cleanup skips these
rows too, because their time is in the future. I ran the same code here
with a correct clock and it accepted the updates.

**Fix (`internal/httpapi/security.go`, `allowPersistentSecurityAction`):**
- A saved start time more than 1 minute in the future starts a new
  counter now (count = 1), and the action is allowed.
- The cleanup also removes rows whose `updated_at` is more than 1 minute in
  the future.
- The normal limits do not change (presence 30 per minute per phone, 120
  per account).
- This heals every counter that uses this code: presence, messages, call
  signaling, call heartbeat, recovery request / status / complete, recovery
  reset, and join relay. Sending messages through the server could get
  stuck the same way. The login lock is separate code and does not change.

**Tests (new `internal/httpapi/clock_heal_223_test.go`):**
- 223-s1: a presence row 3 h in the future with count 30 lets the next
  update through, and the row now starts "now" with count 1.
- 223-s2: a normal row (now, count 30) still refuses update 31.
- 223-s3: a row with `updated_at` 3 h in the future is removed by the
  cleanup.
- 223-s4: a stuck "message-device" counter heals the same way.

**Old tests:** none change.

**You, today:** PC clock on automatic, plus the one-time `fixtime_tool.zip`
(stop the server, run `go run ./cmd/fixtime`, delete the folder, do not
commit it).

---

## B. Phone: the join key from ONE phone (app) - issue 2

**What we saw:** the ZTE never made alex's join box (no `JOIN_BOX_MADE`),
so the Realme had nothing to carry.

**Cause:** a phone that never reached the server took the join key only
when 2 different phones handed the same signed key. With only the Realme
near, that never happens.

**Fix (`join_carrier_222.dart`, `_takeKeyOffer`):**
1. A phone with NO pinned server key takes the key from ONE phone, if the
   note is signed by the server key written inside it (the same check as
   today). Log: `JOIN_KEY_FROM_PHONE n=1`.
2. A phone that already pinned a server key: no change (the key must match).
3. A phone that already has a key: a peer can never replace it (no change).
4. **NEW safety:** when this phone later reaches the real server and the
   server's key is different, the server key wins. The old box is deleted
   and a new one is made with the real key. Log:
   `JOIN_KEY_REPLACED_BY_SERVER`.
5. **NEW:** her box remembers which key made it (`key_id` in
   `join_mine_222`). When the key changes, the box is made again, even if
   her name and password did not change. Today a key change leaves the old
   box behind.
6. The Realme already offers its key to nearby phones (`JOIN_KEY_SAVED`, then
   key offers), so nothing is needed on the carrier side.

**Risk (plain words):** a fake phone could pretend to be a server and hand
its own key. Her box would then be locked to that fake key. The box holds
her password **hash** and recovery-code **hash**, never the real password.
The real server still decides everything, and when she reaches it, rule 4
replaces the fake key. You chose this.

**Tests (new `test/bundle223_test.dart`):**
- 223-1: a never-online phone gets ONE good signed note, so the key is saved
  and `JOIN_BOX_MADE` follows.
- 223-2: a forged note (not signed by its own key) is not taken.
- 223-3: a phone with a pinned key ignores a note with a different server
  key.
- 223-4: the key came from a phone, then the real server shows a different
  key. The server wins, the old box is deleted, and a new box is made.
- 223-5: the same key arrives again, so no new box (no churn).
- 223-6: the full trip with ONLY 2 phones: her box goes to Ali, then the
  server, the answer comes back, and she is joined.

**Old test changed (expectation only):** `test/join_carrier_222_test.dart`,
"never-online phone: one phone is not enough for the join key, two are".
After the first phone it now expects the key (`isNotNull`). It is marked
"Bundle 223", and the second-phone line stays.

---

## C. Phone: "uncaught_async_error" when the server drops (app) - issue 4

**What we saw:** 6 x `uncaught_async_error: WebSocketChannelException ...
Software caused connection abort`, each next to `WS_STREAM_ERROR`.

**Cause:** with web_socket_channel 3, a failed connection also fails
`channel.ready` and `channel.sink.done`. The app listens only to the
stream, so the same error comes back as "uncaught".

**Fix (`lib/core/services/websocket_service.dart`, right after
`IOWebSocketChannel.connect`):** quietly catch the `ready` and `sink.done`
errors. They are already logged once as `WS_STREAM_ERROR` /
`WS_READY_TIMEOUT`. Reconnect, ready-wait and logs do not change.

**Tests:**
- 223-7: a connect to a closed port gives 0 uncaught errors, and the stream
  error is still logged once.
- 223-8 (source check): both `ready` and `sink.done` are handled.

---

## D. Share app: QR text (app, UI) - issue 8

**Mock:** `mock_223_qr_text.png` (left: now, right: 223).

**Why:** on the same Wi-Fi, LinkUp shows only the DOWNLOAD QR (a web link).
You scanned it with Settings -> Wi-Fi -> QR scanner, which reads only Wi-Fi
passwords, so nothing happened. The camera app reads it.

**Change (`share_app_screen.dart`, `_downloadQr`):**
- "Scan with the camera, open in Chrome" becomes **"Scan with the camera
  app, not the Wi-Fi scanner"** (2 centered bold lines) on a soft yellow
  note (yellow fill, thin yellow border, round corners).
- Shown on step 2 AND on the "same Wi-Fi" / "own hotspot" screens (they
  share this card).
- "or type:", the address, and "LinkUp 3.0.63 · 59 MB" do not change.
- The step 1 Wi-Fi QR text does not change (that one IS for the Wi-Fi
  scanner).

**Tests:**
- 223-9: the same-Wi-Fi screen shows the new text, and the old text is gone.
- 223-10: step 1 still shows "Settings -> Wi-Fi -> tap the QR icon".

---

## E. Share app: step 2 has no QR and nothing to tap (app + Kotlin, UI) - Phase 5

**Mock:** `mock_223_step2.png` (now / looking / still nothing).

**What we saw (photo, 07:25):** step 1 has a tick. Step 2 says "Couldn't
find this Wi-Fi's address. Tap the one that starts with 192.168 (not
192.168.49)", but there is NO address to tap and no QR.

**Cause (from the code; no log covers 07:25):**
- Single-radio phones (like the ZTE Blade A35) cannot be on Wi-Fi and run a
  hotspot at the same time. Android runs LinkUp's Wi-Fi on `wlan0` itself.
- The share code always skips `wlan0` (it thinks wlan0 = home Wi-Fi). This
  happens in Kotlin (`AppSharePolicy.pickHotspotIp`) and in Dart
  (`pickShareIp`, `shareIpChoices`). So the address is thrown away and the
  tap list is empty.
- The address is looked for only 3 s (`ADDRESS_WAIT_MS`) plus one quick
  retry.

**Fix:**
1. `wlan0` is accepted when it got a NEW address after LinkUp's Wi-Fi
   started, or when the phone is not on a router and wlan0 holds an `x.x.x.1`
   address (a hotspot always owns .1). The same rule applies in Kotlin and
   Dart. wlan0 on a router (same address as before) is still skipped.
2. Look for the address up to 10 s. On step 2, while nothing is found, show
   "Getting the download address..." with a spinner, and check again every
   2 s. The QR appears by itself.
3. Never "Tap the one..." with nothing to tap. After 10 s, show "Couldn't get
   the address by itself", EVERY private address as a button (with its
   interface name), and **Try again**. Below that: "No address at all? Your
   friend joins the Wi-Fi (step 1), opens its Wi-Fi details, and types the
   Gateway address in Chrome with :8080".
4. Logs: `SHARE_APP ip_wait found=... ms=... iface=...` and `SHARE_APP
   ip_none list=...` (interface names and addresses, for the next log).

**Files:** `AppSharePolicy.kt`, `AppShareNative.kt`,
`share_address_picker.dart`, `app_share_controller.dart`,
`share_app_screen.dart`.

**Tests:**
- 223-11: wlan0 with a new .1 address is picked.
- 223-12: wlan0 on a router (address unchanged) is skipped.
- 223-13: an empty list shows the Gateway text + Try again, never an empty
  "Tap the one".
- 223-14: an address that arrives late shows the QR without leaving the
  screen.
- 223-15: Kotlin `pickHotspotIp` with wlan0 (a source/unit check like the
  210 tests).

**Today's workaround:** your friend scans the step 1 Wi-Fi QR, opens the
Wi-Fi details on their phone, reads Gateway / Router (often `192.168.43.1`),
and types `192.168.43.1:8080` in Chrome.

---

## F. Wi-Fi Direct tags never get through (Kotlin) - Phase 6

This is the reason the two phones cannot find each other without a router,
and so alex's card never reached the Realme.

**What we saw:** in all 4 logs, every Wi-Fi Direct search ends with
`heard=0`. Each phone receives only the short service name "LocalLink" (PTR,
`WD_TAG raw kind=ptr`), never the tag details (TXT). Pair then waits for a
tag that never comes (`PAIR_AMBIGUOUS count=0`).

**Also seen right after the account recovery (Realme, 07:37-07:41):** the
Realme's Wi-Fi was OFF from 07:37:52 to 07:39:00 and from 07:39:17 to 07:40:23.
"Pair" with the ZTE was tapped 3 times in those gaps (07:38:14, 07:38:33,
07:40:00), and all 3 failed with `PAIR_AMBIGUOUS count=0`. The ZTE WAS seen
by Wi-Fi Direct (`WD_PEERS n=1 ZTE Blade A35`), but its tag was not
(`heard=0`). The Wi-Fi came back at 07:40:23, the ZTE showed Nearby over the
router at 07:40:49, and Pair worked at 07:41:01 (over the router). So adding
friends works today ONLY when both phones are on the same router.

**Cause:** Android sends a service in 2 parts: the name (PTR) and the
details (TXT). On many phones the TXT part is lost or never asked for. This
is a known Android problem (noted earlier from Stack Overflow 26300889 /
21722767). Our name is always the same ("LocalLink"), so the name alone says
nothing about which phone it is.

**Fix (2 steps, the tag format does not change for phones that DO get TXT):**
1. **Ask for the details directly:** when a PTR arrives from a phone and no
   TXT follows within 2 s, send a second, exact request for that phone's
   service (`WifiP2pDnsSdServiceRequest.newInstance(instanceName,
   serviceType)`). Many phones answer this one. At most once per phone per
   search window. Log: `WD_TAG txt_ask from=...`.
2. **A short tag in the name itself:** the published name becomes
   `LinkUp-<tag id 8>-<flags>` (still under 63 characters). The hidden
   service type `_locallink._tcp` stays, because older phones search for
   it. They never check the name, so "LinkUp" is safe. A phone that
   gets only the PTR can still know WHICH LinkUp phone it is (id, host or
   member, wants link), enough for Pair and to connect. Old phones
   (222 and before) never read the name, so they still work. A name that
   is just "LocalLink" (an old phone) is still accepted. The full TXT tag stays as it is. Log: `WD_TAG from_name id=...`.
3. Pair: when only a name-tag is known, Pair uses it (no more
   "ambiguous, count 0" when one LinkUp phone is clearly there).

**Files:** `NearbyTagManager.kt`, `mesh/NearbyTagPolicy.kt` (decode name
tag). No Dart change unless Pair needs the new "from_name" flag (then
`nearby` pair code, 1 file, will be listed in APPLY).

**Risk:** this touches the core Nearby search. Test order as always: router
echo, then Wi-Fi Direct, then mesh. If you want it safer, do F in its own
bundle (224) after 223 is tested.

**Tests:**
- 223-16: NearbyTagPolicy encodes and decodes the name tag, and a bad name
  gives null.
- 223-17: an old name ("LocalLink" only) is still accepted as before.
- 223-18 (source check): a PTR without TXT leads to one exact TXT request.

---

## G. App admin: offline accounts learned only from cards (no code, explanation)

The Realme admin list "Offline · not on server yet" learns an account only
from that phone's profile card, received nearby. At 07:44 and 07:50 the
Realme had 1 offline row (`USERS_LOCAL_ROWS n=1`). That is probably the
ZTE's OLD account from before the clear data. With A, B and F fixed and the
phones linked, alex shows there and on the server.

**What you can do today:**
1. ZTE: turn **Wi-Fi ON** (it went off after the clear data), and give
   LinkUp's **Nearby devices** permission again.
2. Put both phones on the **same router Wi-Fi**. That road works today.
3. Open LinkUp on both and wait until each shows the other as **Nearby**.
4. Realme: open **Admin -> Users** and tap **"Not on server"**.
5. If it still does not show, send logs from both phones taken right after
   this.

---

## H. Phone: WS reconnect stuck up to 65 s (app) - Phase 2, NEW

**Log (Realme):** gen2 07:41:04 -> ready 07:42:09 (65 s); other tries 28 s,
60 s, 47 s.
**Cause:** `_scheduleReconnect` calls `_open(waitForReady: false)`; the 8 s
ready limit only applies when the app waits. A background try waits for
Android's own connect timeout (re-sends at 1, 3, 7, 15, 31, 63 s).
**Fix (`websocket_service.dart`):**
- Every try (also background) gets a limit: no `ready` in 10 s -> log
  `WS_RETRY_TIMEOUT generation=N after_ms=10000`, close, schedule the next try.
- New `networkBack()` call (from the network watcher, `NET_CHANGE` up): a
  hanging try is dropped and a new one starts at once; the back-off resets.
**Tests:** 223-19 a channel that never sends `ready` is closed after the
limit and a retry is scheduled; 223-20 `networkBack()` replaces a hanging try.

## I. Phone: permission pop-ups one at a time (Kotlin + app) - Phase 3, NEW

**Log (ZTE):** 07:47:36.156 `Can request only one set of permissions at a time`.
**Cause:** `app_permission_gate.dart` waited a fixed 5 s, not for the answer
(`gateRequest` replied at once). Android DROPS a second request while a
pop-up is open. Other askers (Allow card, Add friend, Wi-Fi Direct helper,
notification screen, background location) had no shared lock.
**Fix:**
- `MainActivity.kt`: one queue for every runtime-permission pop-up. A request
  while one is open waits in the queue and fires after the answer (never
  dropped). Logs `PERM_ASK name=.. code=..`, `PERM_ANSWER name=.. granted=y/n`,
  `PERM_QUEUED name=.. behind=..`.
- `gateRequest` replies to Dart only after the answer (or after the battery
  screen closes: on resume), with a 60 s safety limit.
- `app_permission_gate.dart`: waits for each answer, then 1 s, then the next.
  Order, no own pop-up, re-ask every 10 min: unchanged (bundle 101 rules).
- `LocalLinkNotificationManager.kt`, `CallNotificationManager.kt`: their
  requests go through the same queue.
**Tests:** 223-21 the gate waits for a late answer before the next request;
223-22 the gate moves on after its safety limit; 223-23 (source) every
`requestPermissions(` in Kotlin goes through the queue.

## J. Phone: password lock off the screen thread (app) - Phase 3

**Log (ZTE):** 07:47:28 `MAIN_STALL ms=6302` while making the offline account.
**Cause:** `PasswordKdf.derive` (180,000 rounds) runs on the screen thread.
**Fix:** `PasswordKdf.deriveAsync` / `verifyAsync` run the same maths in a
background isolate (`Isolate.run`); same result, same 180,000 rounds, old
hashes keep working. Callers in `authentication_repository.dart` and
`recovery_repository.dart` use them. Log `KDF ms=..`.
**Tests:** 223-24 async == sync for the same salt; 223-25 an old hash
verifies with `verifyAsync`; 223-26 (source) no sync `derive(`/`verify(` left
in the two repositories.

## K. Phone: LAN "Network unreachable" every 4 s (Kotlin) - Phase 4, NEW

**Log (ZTE):** 48 x `sendto failed: ENETUNREACH` in 3 min with Wi-Fi off.
**Cause:** `lanBroadcastAddresses()` falls back to 255.255.255.255; every
beacon fails and sends an error event to Dart.
**Fix:** `MeshTransportPolicy.lanBeaconBackoff`: after an unreachable error
the beacon waits 15 s and the error is reported ONCE (`LAN_BEACON paused
why=no_network`); a good send or `NET_CHANGE` brings the normal tier back
(`LAN_BEACON resumed`).
**Tests:** 223-27 policy: error -> 15 s and report once; ok -> normal tier.

## L. Phone: no server calls when there is no network (app) - Phase 4, NEW

**Log (Realme):** `MESSAGE_SYNC_FAILED` 10 s x5, `CONTACTS_SYNC_FAILED` 6 s
x4, version rule / admin badge 8 s, all while Wi-Fi was off.
**Fix:** a small shared gate `ServerReach` (no network, or the policy probe
says unreachable -> skip with one INFO line `SERVER_CALL_SKIPPED what=..
why=..`). Used by message sync, contacts sync, version rule and admin badge
sync. When the server comes back they run as before.
**Tests:** 223-28 `ServerReach` says skip with no network / unreachable and
go when back; 223-29 sync is skipped (no API call) while offline.

## M. Phone: a new chat opens at once when the server is down (app) - Phase 4, NEW

**Log (Realme):** chat opened 07:41:07.9, saved messages returned 07:41:21.
**Cause:** `history()` waits for `synchronize()` (10 s) and `api.messages`.
**Fix:** when the socket is not ready, `history()` returns the saved
messages at once and starts the sync in the background.
**Tests:** 223-30 with the server down `history()` returns within 1 s.

## N. Phone: phone-only account does not call the server (app) - Phase 4, NEW

**Log (ZTE):** `ADMIN_BADGE_SERVER_SYNC failed ... Server address is required`,
`VERSION_RULE_SERVER_FAIL` (same).
**Fix:** with no server address these skip quietly (one INFO line
`SERVER_CALL_SKIPPED why=no_server`). Part of the `ServerReach` gate (L).
**Tests:** 223-31 no server address -> skip, no error line.

## O. Logs cleanup (app) - Phase 7

- `APP_BUILD_STAMP`: INFO, built from `kAppVersion` + `kAppBuild`.
- PAIR_STAGE / PAIR_UI / PAIR_TAP / PAIR_SHEET / PAIR_ACCEPT: INFO.
- `KNOWN_PEERS_REFRESH_SKIPPED` / `MESSAGE_SYNC_SKIPPED_NO_TOKEN`: once per
  state change, INFO.
- `DIRECT_TRANSPORT_INFO`: only when a value changes.
- `ACCOUNT_OFFLINE_BOX`: `reason=phone_only|no_network|server_down`.
- Offline timeouts: WARN, not ERROR.
**Tests:** 223-32 stamp text == `kAppVersion`; 223-33 (source) the pair
lines use info; 223-34 the repeated-line helper logs once per state.

## P. Watch items (Phase 8)

- `LINK_AUTH` adds `peer_has=..` (which key ids the peer offered) to the
  `no_shared_key` line, so the next log shows which side lost the key.
- Admin badge `no_trust` once after recovery, server back late, ZTE 3.2 s
  first-draw stall, Android system lines: no code.

## Q. Found while fixing

Each item has its own test.

- **Q1 (backend, Phase 1):** a login lock written while the server clock was
  ahead could last for days (`locked_until` far in the future). Now a lock
  longer than 15 min + 1 min is ignored, and a "last failed login" time in
  the future starts the count again. Test 223-s5/s6 in
  `clock_heal_223_test.go`.
- **Q2 (Kotlin, Phase 3):** `gateRequest` replied to Dart BEFORE the user
  answered, so the app fired the next pop-up while one was open and Android
  dropped it (ZTE 07:47:36). Now the reply comes after the answer (60 s
  limit). Tests `Perm223Test` + 223-21/22/23.
- **Q3 (Kotlin, Phase 3):** background location (7815) and the camera ask
  called Android directly, outside the queue. Now every ask goes through
  `askPermissions` -> `PermissionQueue` (`fireAsk` is the only place left
  that calls Android). Source check 223-23.
- **Q4 (app, Phase 3):** `mirrorOfflineLogin` ran the password lock on the
  screen thread after EVERY online login (~6 s freeze on the ZTE). Now it
  runs off the screen thread. Tests 223-25b + 223-26.
- **Q5 (Kotlin test, Phase 3):** old `Call189Test` counted 3
  `lastGroupChangeAt` lines; bundle 205 added 2 more, so it failed since 205.
  Expectation set to 5 (only the number changed).
- **Q6 (app, Phase 4):** the hotspot / Wi-Fi Direct host address must still
  be allowed while the socket is up, even when "no internet" is reported.
  The gate says "go ahead" whenever the socket is ready. Test 223-28
  ("a connected socket always goes").
- **Q7 (app, Phase 4):** `history()` returns the local messages first ONLY
  when the socket is not ready; with the socket up it still waits for the
  server copy (no change for online chats). Test 223-30.
- **Q8 (app, Phase 4):** the old `HttpVersionRuleServer` check
  `baseUrl().isEmpty` never worked: `baseUrl` throws `FormatException` on an
  empty address, so a phone-only account logged an error every check. The
  gate now skips first. Test 223-31 (source check).
- **Q9 (app, Phase 4):** `ServerReach.bind()` now resets the gate on every
  sign-in, so a "server down" from the last account does not block the next
  one. Test 223-28 ("bind() starts with an open gate").
- **Q10 (app, Phase 5):** the old step 2 could show "Tap the one..." with an
  EMPTY list (no address passed the filter). Now it is the spinner while
  looking, then every address + Try again + the Gateway tip. The old
  `shareIpChoices` is unchanged (bundle 210c tests); the new `shareIpAll` is
  used only after 10 s. Tests 223-12/13/14.
- **Q11 (app, Phase 5):** the immediate recheck is kept and
  `ADDRESS_WAIT_MS` stays 3 s (the first look is not slower). The wait uses
  a tick counter, not a Stopwatch (a Stopwatch does not move in tests and
  would never time out). Test 223-13.
- **Q12 (app, Phase 5):** `SHARE_APP ip_wait` / `ip_none` are written from
  Dart (once each), and a new `SHARE_APP ip_picked` shows which address was
  used; one fresh non-wlan0 address is preferred over wlan0. Test 223-14.
- **Q13 (app, Phase 6):** a name tag has no group name / password, so it is
  kept OUT of the group planner and out of `pairTap` (a silent join without
  the password would fail and could pop up a system dialog). Name tags only
  fill the nearby list. Tests 223-16e (source) + 223-16c.
- **Q14 (app, Phase 6):** `pickPairTarget` compared ids exactly, so an 8-char
  id from a name tag never matched the 32-char id. Now a prefix match.
  Test 223-16c.
- **Q15 (app, Phase 6):** `planNearbyList` compared exact ids too, so a
  FRIEND's (or a BLOCKED person's) name tag would show as "new, Add friend".
  `fullTagId()` now turns an 8-char id into the known full id first.
  Test 223-16d.
- **Q16 (Kotlin, Phase 6):** name tags got the same 5-min stale cleanup as
  full tags, and an unchanged name tag is sent to Dart again every 30 s
  (otherwise the "seen" time in Dart goes old and the person drops off the
  list while still near). Test 223-18b in `Tag223Test`.
- **Q17 (app, Phase 7):** `KNOWN_PEERS_REFRESH_SKIPPED` and
  `MESSAGE_SYNC_SKIPPED_NO_TOKEN/NO_SERVER` were a WARNING on EVERY sync of
  a phone-only account. Now once per state (new `LogOnce` helper), INFO.
  Test 223-34.
- **Q18 (app, Phase 7):** the `APP_BUILD_STAMP` text was typed by hand and
  stayed at "3.0.50" from bundle 209 to 222, so every log said the wrong
  version. Now it is made from `kAppVersion` + `kAppBuild` (`AppBuild.stamp`)
  and a test keeps it equal to pubspec. Test 223-32.
- **Q19 (app, Phase 7):** 6 network failures were ERROR even when the phone
  was simply offline: `MESSAGE_SYNC_FAILED`, `MESSAGE_HISTORY_REMOTE_FAILED`,
  `MESSAGE_REALTIME_CONNECT_FAILED`, `HTTP_REQUEST_FAILED`,
  `WS_STREAM_ERROR`, `WS_READY_TIMEOUT` (21 ERROR lines in the Realme log).
  New `AppLogger.failure`: offline (no route / refused / timeout) = WARN with
  `offline=<type>`; any other error stays ERROR. Test 223-34b.
- **Q20 (app, Phase 7):** PAIR_* normal steps were WARN (25 lines per pair).
  Now INFO; `adopt_failed`, `caught error=` and `PAIR_AMBIGUOUS` stay WARN
  because they are real problems. Test 223-33.
- **Q21 (Kotlin, Phase 5):** the 15 s "hotspot start" limit also covered the
  address wait after the hotspot was already on. A slow start plus the wait
  could pass 15 s, and the timeout then turned the working hotspot OFF.
  Now the limit covers only the start (`hotspotUp`). Test `Share223Test`.
- **Q22 (Kotlin + app, Phase 5):** with the new wlan0 rule, a router joined
  during the wait could look like a "new" wlan0 address. The router's own
  address (`wifi_ip`) never counts as the hotspot. Test `Share223Test`
  ("a router joined during the wait is not the hotspot").
- **Known limit:** `verifyOfflinePassword` / `verifyOfflineLogin` stay as they
  are (their contract cannot change); the app calls the new
  `verifyOfflineLoginAsync` instead. The rename confirm in `home_screen.dart`
  still checks on the screen thread (rare, left as is).

---

## Files

**Backend (deploy first):**
- `internal/httpapi/security.go` (A, Q1)
- `internal/httpapi/clock_heal_223_test.go` (new)
- `TODO_223.md`

**App - in this TODO from the start:**
- `lib/features/authentication/data/services/join_carrier_222.dart` (B)
- `lib/core/services/websocket_service.dart` (C, H, Q19)
- `lib/features/share_app/presentation/share_app_screen.dart` (D, E)
- `lib/features/share_app/domain/share_address_picker.dart` (E)
- `lib/features/share_app/domain/app_share_controller.dart` (E)
- `android/.../AppSharePolicy.kt`, `android/.../AppShareNative.kt` (E)
- `android/.../NearbyTagManager.kt`, `android/.../mesh/NearbyTagPolicy.kt` (F)
- `android/.../MainActivity.kt`, `LocalLinkNotificationManager.kt`,
  `CallNotificationManager.kt`, `lib/core/permissions/app_permission_gate.dart` (I)
- `lib/core/crypto/password_kdf.dart`,
  `lib/features/authentication/data/repositories/authentication_repository.dart`,
  `lib/features/recovery/data/repositories/recovery_repository.dart` (J)
- `lib/core/config/app_build.dart`, `pubspec.yaml` (3.0.63+134, O)
- `test/join_carrier_222_test.dart` (1 expectation: `isNotNull`, B)
- `test/nearby_190_test.dart` (version line, as in every bundle)
- `TODO_223.md`, `APPLY_223.md`

**App - more files the fixes needed (told to you):**
- `android/.../PermissionQueue.kt` (new, I),
  `lib/features/authentication/bloc/authentication_bloc.dart` (J, Q4)
- `android/.../mesh/MeshTransportPolicy.kt`,
  `android/.../LocalLinkTransportService.kt` (K)
- `lib/core/services/server_reach.dart` (new, L/N),
  `lib/features/messaging/data/services/reliable_messaging_service.dart`
  (C, L, M, O), `lib/app/app_dependencies.dart`,
  `lib/features/directory/data/repositories/directory_repository.dart`,
  `lib/features/admin/data/services/version_rule_service.dart`,
  `lib/features/admin/data/services/admin_badge_service.dart` (L/N)
- `lib/features/connectivity/data/models/nearby_tag.dart`,
  `lib/features/directory/presentation/nearby_list_plan.dart`,
  `lib/features/connectivity/data/services/wifi_direct_fallback_service.dart` (F)
- `lib/app/local_link_app.dart`, `lib/core/services/app_logger.dart`,
  `lib/core/network/api_http_client.dart`,
  `lib/features/directory/data/services/pairing_service.dart`,
  `lib/features/directory/presentation/nearby_people_section.dart`,
  `lib/features/account/presentation/screens/account_center_screen.dart` (O)

**Tests (new):**
- Dart: `test/bundle223_join_test.dart`, `bundle223_ws_test.dart`,
  `bundle223_share_test.dart`, `bundle223_first_start_test.dart`,
  `bundle223_offline_test.dart`, `bundle223_nearby_test.dart`,
  `bundle223_logs_test.dart`
- Kotlin: `Perm223Test.kt`, `Share223Test.kt`, `mesh/LanBeacon223Test.kt`,
  `mesh/Tag223Test.kt`; old `Call189Test.kt` 3 -> 5 (Q5)

---

## Phone tests (after push; backend first)

1. **223-A clock:** the Realme log has NO "presence update rate limit
   exceeded".
2. **223-B carry with 2 phones:** clear the ZTE's data and make offline
   "alex2" (no server). With the Realme near, the ZTE log shows
   `JOIN_KEY_FROM_PHONE`, then `JOIN_BOX_MADE`. Bring the Realme to the
   server: `JOIN_RELAY_UPLOADED` appears, the ZTE gets the answer, and alex2
   is in Admin -> Users (server).
3. **223-C:** turn the server off for 1 minute. The log has
   `WS_STREAM_ERROR` but NO `uncaught_async_error`.
4. **223-D:** both phones on the same Wi-Fi, open Share app. The yellow note
   says "Scan with the camera app, not the Wi-Fi scanner". Scan it with the
   CAMERA and the page opens.
5. **223-E:** on the ZTE, Wi-Fi ON but not connected, open Share app. Step 2
   shows the QR (or the address buttons + Try again), never an empty "Tap
   the one".
6. **223-F:** both phones OFF any router, LinkUp open on both. The log shows
   `heard=1` or more (or `WD_TAG from_name`), and Pair works within 1
   minute.
7. **223-G:** alex is shown on the Realme in Admin -> Users -> "Not on
   server", and after the carry, on the server.
8. **223-H:** with the server off, then on again: the log shows
   `WS_RETRY_TIMEOUT` at most 10 s after a try, and the socket is back
   within ~15 s of the server coming up (no 65 s gap).
9. **223-I:** clear the ZTE's data and open the app: every permission
   pop-up appears one after the other (`PERM_ASK` / `PERM_ANSWER` pairs, no
   missing answer); none is skipped.
10. **223-J:** log in on the ZTE: NO `MAIN_STALL` of 6 s around the login.
11. **223-K:** Wi-Fi off for 1 minute: `LAN_BEACON paused` once, not 15
    "Network is unreachable" lines; `LAN_BEACON resumed` after Wi-Fi is on.
12. **223-L/M/N:** Wi-Fi off: `SERVER_CALL_SKIPPED what=.. why=no_network`
    once per kind; opening a chat shows the messages at once.
13. **223-O:** the first log line is `APP_BUILD_STAMP v3 · 3.0.63+134`
    (INFO); a pair shows PAIR_* as INFO; offline sync failures are WARN.

---

## Not in 223 (stay in PENDING.md)

- The old offline row of a cleared phone stays in the admin list (it could
  be hidden after N days; say if wanted).
- Older open items (block fix, Wi-Fi call gaps, Realme mic, mesh voice,
  video, group voice, notification picture, "My QR" place).
