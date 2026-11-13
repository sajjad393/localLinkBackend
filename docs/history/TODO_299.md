# TODO 298 + 299: complete plan (freeze ON, nothing is built until "Build")

Written 14 Nov from the code at: app `23c09b7` (3.0.94+165) + `~/wip_298_app.patch` + `~/wip_299_app.patch`,
backend `94966fc` (297) + `~/wip_299_backend.patch`. Mocks: `mocks/app_298.html`, `mocks/app_298_part2.html`, `mocks/app_299.html`.
Detail notes: `TODO_298.md`, `TODO_299.md`. Restore the work after a wipe: `bash ~/setup299.sh`.

## 0. Order, versions, rules

| Step | What | Version | Depends on |
|---|---|---|---|
| 1 | App 298 (A-I) | 3.0.95+166 | "Build 298" |
| 2 | Backend 299 (N, S, A, B, server half of C) | backend 299 | "Build 299". Deploy before app 299 |
| 3 | App 299 (C, Q, R) | 3.0.96+167 | backend 299 on the server |

- One zip + one commit per step. The backend zip is separate and goes first. Never push. CI is non-blocking.
- One defect at a time, smallest change, a regression test for each part. Old tests unchanged except the ones listed here.
- Keep http (no https). No new packages or modules (Go: standard library only; app: packages it already has).
- Mocks are not hardware: every part ends with a phone check list in the APPLY doc.

---

# PART 1: App 298 (3.0.95+166), prototype DONE, waiting for "Build"

Status: all of A-I are in `~/wip_298_app.patch` (base `23c09b7`). Lock run 611 passed, analyze 0 errors.

| # | What the user sees (mock) | Code (files) | Test |
|---|---|---|---|
| A | Big map: "Full screen" button under zoom. Map fills the screen, status and navigation bars hidden, "Exit full screen" pill. Back leaves full screen first | `link_paths_full_screen.dart` (`_full`, `_setFull` immersiveSticky, PopScope, `_frameGroup` on enter/exit, dispose restores the bars) | new `test/search_map_298_test.dart` (to write at build: open/exit/Back/bars restored) |
| B | Every search is one grey pill with a 2 px round ring while typing. No blue box inside | new `core/widgets/search_pill_298.dart`, used in `chat_search_bar.dart`, `chats_search.dart`, `new_group_screen.dart`, `group_info_screen.dart` (Add members), `forward_screen.dart`, `contacts_screen.dart` (People), `admin_list_page.dart`. `admin_kit216.dart` `AdminSearch216` keeps a ring at all times and makes it clear when not focused (fixes a typing bug the tests found). `chat_screen.dart:1104` old field: borders none | `search_map_298_test.dart` (pill ring on focus, no inner border, typing works) + lock `admin_users_offline_222` |
| C | Incoming / in-call crown no longer covers "LinkUp voice call" | `incoming_voice_call_body.dart`, `call_header.dart` (photo inside `AdminCrownRoom`) | `test/call_crown_298_test.dart` (5 screen sizes) |
| D | Small map: people on both sides of You. Pinch 0.5x-4x. "Reset" pill only after a zoom/move. One finger scrolls Chats at normal size | new `link_balance_298.dart`, new `mini_map_zoom_298.dart`, `link_paths_map.dart` (`_balanced298` when small), `my_links_map_273.dart`. Admin small card gets only the spread | `test/mini_map_298_test.dart` 8/8. Listed change: sha in `test/link_paths_zoom_222_test.dart` -> `2365d7d5...` |
| E | No way divider lines in chat. Tiny way icon before the time on received messages; tap = toast "Came by ..." Long press > Info keeps the full way | new `route_mark_298.dart`, `message_bubble.dart` `_footer`, `chat_screen.dart` (old divider code removed) | `test/chat_calls_298_test.dart` |
| F | WhatsApp-style call bubbles in the chat (old calls too). Tap = call back | new `call_bubble_298.dart`, `chat_screen.dart` (`_callsSub298`, `_calls298`, `_callRows298`) | `test/chat_calls_298_test.dart` (17/17 with E and I) |
| G | Clock card shows 3 s each time the screen opens, then slides away | `admin_kit216.dart` `ServerClockBar255` | lock `bundle255_clock` (unchanged, passes) |
| H | Crown tip stays 7 s | `crown_tip_290.dart` `shownFor` | listed change: `test/crown_tip_290_test.dart` (6 s shown, 7 s closed) |
| I | Recovery code to WhatsApp = a designed card picture (1080x1080), caption = only the code | new `recovery_card_298.dart`, `recovery_code_save_235.dart` (`openCard`, `clearCard`, `caption`), `MainActivity.kt` (`shareImageToWhatsApp298`, `clearShare298`), new `ShareFileProvider298.kt`, `res/xml/share_paths_298.xml`, manifest provider `${applicationId}.share298` | `chat_calls_298_test.dart`. Lock `bundle235` unchanged |

**Open (298-I):** if the picture cannot be made, the text-only fallback still sends the old line. Recommended: send only the code there too (one rule everywhere). Waiting for the answer. Default if no answer: only the code.

**298 build steps (after "Build"):**
1. `bash ~/setup299.sh`, delete every `test/zz_*`, write `test/search_map_298_test.dart` (A + B).
2. `bash ~/m298/locks_ei.sh` (must print 611+ passed, LOCKS_DONE), full suite in chunks `~/run_chunks_tc.sh`, `flutter analyze` 0 errors.
3. `~/bump.py` -> 3.0.95+166. `rm -rf android/.kotlin`. `~/mkzip.py` -> one app zip.
4. `~/APPLY_app_298.md` (phone checks for A-I), update this file + `PENDING.md`, PowerShell block (app: `git add -A` + `git add -f` for `.md`, commit `v3.0.95 ...`).

---

# PART 2: Backend 299, prototype partly DONE

New Go files go in `internal/httpapi/` (routes in `cmd/server/main.go`) plus the new package `internal/mdns299`.
Migration: next is **62** (`internal/store/store.go`, last is 61 "recovery-kind-offline-seen-join-relay").

## N. Short names, no port (user: "both"; port: "no user add")

What people type: `linkup.local` = admin panel, `linkup.local/download` = download page (Android 12+, Windows 10/11).
`linkup` and `linkup/download` work on every phone once the router has the name. The IP always works.

1. **Port 80** (`cmd/server/main.go` near the `srv` setup, line ~230): a second `http.Server{Addr: ":80", Handler: srv.Handler}`
   with the same timeouts. It runs only when `LOCALLINK_ADDR` is the default `:8080` and `LOCALLINK_HTTP80` is not `off`. If it fails
   (port busy: IIS/http.sys, Skype, XAMPP), log `PORT80_299 off: <err>`; 8080 is not affected. Windows needs no admin
   rights for port 80. Apps keep using 8080 (no app change).
2. **Name** `internal/mdns299` (prototype DONE: 5 tests pass, a live query on Linux got the right answer): answers `linkup.local` with the PC's IPv4
   on the network the question came from. IPv6 questions get a short "IPv4 only" answer, so phones do not wait. Android's one-shot
   query gets the answer it expects. Start it next to `discovery.Start` (main.go:312). Env `LOCALLINK_MDNS_NAME`
   (default `linkup.local`, empty = off). Failure = log `MDNS_299 off: <err>`; the server is not affected.
3. **Routes** (main.go): `GET /{$}` -> 302 `/admin/`. `GET /download`, `/Download`, `/download/`, `/linkup`,
   `/LinkUp`, `/linkup/` -> the download page (B). `GET /download/{file}` + `/linkup/{file}` -> the APK (B).
4. **Status** in `AdminHealth` (`internal/httpapi/admin_overview.go:374`): `port80` (on/off + reason) and `short_name`
   (on/off). The web Health page shows "Phones must type :8080" when port 80 is off.
5. The page and the web show `linkup.local/download` (field `Short`) only when the name is on. The QR always carries the IP link
   `http://<ip>/download` (`:8080` when port 80 is off), so Android 10/11 phones also work.
6. Not possible, as told to the user: `linkup.app` (Chrome forces https for every .app address); a bare `linkup` without router DNS.
7. **Router (user's job, APPLY doc):** DHCP reservation for the server PC (192.168.18.27) + a local DNS / static host
   entry `linkup` -> 192.168.18.27. Exact steps once the user says the router brand.
- Tests: `internal/mdns299/mdns_test.go` (done); `port80_299_test.go` (busy port -> logged, 8080 still serves); routes test
  (`/` redirect, all six page paths, case).
- Not verified: Windows port 5353 next to the Windows DNS client, Android 12 phones. Phone check in the APPLY doc.

## S. Admin panel speed (code measured fast: 2 ms per API. Waiting for the user's `duration=` log lines)

1. **gzip + browser cache** for `/admin/*` assets (`internal/httpapi/admin_web.go`, `AdminWeb`, `Cache-Control: no-store` at ~:59):
   pack each embedded file once at start (gzip and its sha256). Send `Content-Encoding: gzip` when the browser asks,
   `ETag` + `Cache-Control: no-cache`, and `304` on `If-None-Match`. 383 KB -> 98 KB; the second open downloads almost nothing.
   Fonts (`admin_216.go`) are already cached. `index.html` keeps the same CSP.
2. **"last seen" write at most once a minute** per admin session (`admin_auth.go:306`
   `UPDATE admin_sessions SET last_seen_at`): skip when the stored value is less than 60 s old. The 180-day idle rule (Bundle 230) is
   unchanged (1 minute does not matter there).
3. Not changed: `SetMaxOpenConns(1)` (`store.go:45`, safe SQLite writes). Revisit only if the log shows waits.
- Tests: `admin_speed_299_test.go`: gzip on/off, ETag 304, a changed file gets a new ETag, last_seen written once in 60 s.
  Locks: `admin_web_216/217`, `admin_web_test` (asset types).

## A. Web "App download" page (owner only). Web prototype DONE (`views.js` view `apkdownload`, `.apk299-*` CSS)

1. **Table** (migration 62) `app_apk_299`: id, version, build, file_name, size, sha256, cert_sha256, added_at, added_by,
   downloads, removed_at, removed_by, why (`replaced`/`removed`), current (0/1). The signing key of the first APK is kept as `cert_sha256`
   of the first row (it never changes).
2. **Files**: `data/apk/` (next to `LOCALLINK_FILES_DIR` default `data/files`, `api.go:79`). Only the current APK is kept.
   Upload goes to `data/apk/tmp-*`, then a rename, then the old file is deleted. Temp files are cleaned at start.
3. **API** (owner = `requireAdminPermission(w, r, "manage_settings")` + `Role == "owner"`, like `requireOwnerChat`):
   - `GET /api/v1/admin/apk299` -> `{current|null, link, short, qr_svg, history[]}`.
   - `POST /api/v1/admin/apk299`: **multipart/form-data** field `apk`. Multipart is already skipped by the 2 MB
     generic cap (`limitRequestBody`, main.go:504), and the handler sets its own `MaxBytesReader` 300 MB. XHR upload progress.
     Longer read deadline for this request (`http.NewResponseController(w).SetReadDeadline(now+30 min)`), because the server
     `ReadTimeout` is 5 min.
   - `DELETE /api/v1/admin/apk299` -> file deleted, history row `why=removed`.
   - Registering these fixes the prototype failure in `TestAdminWebOnlyCallsRegisteredEndpoints` (expected now).
4. **APK checks** (new `apk_check_299.go`, standard library: `archive/zip`, `crypto/x509`):
   a) is a zip with `AndroidManifest.xml`; b) small binary-XML reader -> package, versionCode, versionName;
   c) package == `com.sajjad.linkup`; d) build > current build; e) signer cert from the v2/v3 APK signing block
   (v1 `META-INF/*.RSA` fallback) == the first APK's cert; f) <= 300 MB. Each refusal = a plain sentence (the mock list:
   "This is not LinkUp", "must be newer than 3.0.96", "signed with a different key ...", "not an APK", "Too big (max 300 MB)",
   "The server disk is full"). The old APK stays.
5. **QR** `qr_svg`: small QR encoder in Go (new `qr_299.go`, byte mode, level M, about 300 lines, own code, no module).
   Test: encode, then decode it in the test with a tiny reader, or check against fixed known matrices.
6. **Audit** `adminAudit(admin.ID, "apk_added"|"apk_removed", file, "v=<version> b=<build>")` (`admin_auth.go:367`).
7. **"phones on an older LinkUp"** = `device_builds` (version_rule.go:272) rows seen in the last 30 days with build < current.
8. Web: prototype view as in the mock (sidebar System > App download, `roles: ['owner']`). Link box: `short` big + IP link
   + "Older phones (Android 10/11)". Remove asks first (`confirmDialog` `destructive`).
- Tests: `apk299_test.go`: owner only (operator/auditor 403), upload ok / not LinkUp / older / other key / too big /
  not a zip, replace deletes the old file, remove, history order, download count, audit rows. Test APKs are built in the test
  (zip + a hand-made binary manifest + a test-signed signing block).

## B. Public download page (no login). Page prototype DONE (`linkup_page_299.go`)

1. `GET` page paths (N.3) -> `linkupPage299` (light/dark, EN + Urdu, Noto Naskh for Urdu, QR, `Short`). No APK yet = "LinkUp is
   not on this server yet".
2. `GET /download/{file}` (+ `/linkup/{file}`): only the current file name, `http.ServeContent` (resume works),
   `application/vnd.android.package-archive`, `Content-Disposition: attachment`. Longer write deadline for this
   request (`SetWriteDeadline(now+60 min)`), because the server `WriteTimeout` is 5 min (a 106 MB file on weak Wi-Fi takes longer).
   `downloads + 1` once per IP per 10 min (memory map, not a DB write per request).
3. Only GET/HEAD; rate limit per IP (`allowSecurityAction`); page CSP `default-src 'none'; style-src 'unsafe-inline'; img-src data:`.
   The version gate already lets these paths through (`version_gate.go`: not under `/api/v1/`).
- Tests: `linkup_page_299_test.go`: page with / without APK, escaping, Short shown only when on, file Range 206,
  wrong file name 404, POST 405, download count once per IP.

## C (server half). "Which LinkUp is on the server"

1. `GET /api/v1/app/latest` (device sign-in) -> `{version, build, size, sha256, file}` or `204`. Read from a memory
   copy (updated on upload/remove), so no DB read per phone.
2. **Must be added to `versionGateExempt`** (`version_gate.go`), or a stopped phone (426) could never get the update
   that un-stops it.
- Tests: 204 without APK, 200 fields, gate exempt (old build header still gets 200).

---

# PART 3: App 299 (3.0.96+167), prototype partly DONE

## C. Update from the server first, nearby phones second (user: "both")

1. New `lib/features/share_app/data/app_latest_server_299.dart` like `HttpVersionRuleServer`
   (`version_rule_server.dart`): `BuildHeaderClient`, `baseUrl: () => api.baseUrl`, device id + token headers, 8 s timeout.
2. `SilentUpdate275` (`share_app/domain/silent_update_275.dart`): new optional `server` source (injected in
   `app_dependencies.dart:1244` `_startSilentUpdate275`).
   `check()`: server first. If `build > kAppBuild` (and >= `minBuild`), the same day gate (`gateKey`) and the same popup with
   `fromServer = true` ("New LinkUp on the server", "Getting it from the server ...": prototype done in `update_sheet.dart`).
   Download `${api.baseUrl}/download/<file>` with the existing `_httpDownload` (size limit), then **sha256** (package `crypto`,
   streamed) must equal the server's, then `native.checkApk` (size + package, `UpdateInstall275.check`), then Ready.
   Server down / wrong file -> the old phone path `_runAll(list)` as today.
3. When: the 20 s first look stays. The server is asked at most every 6 h, plus once after the app reconnects to the server.
   The 60 s timer keeps the phone path only. Same 275 rules: once per build per day, not in a call, Cancel = not today, the file
   is deleted after the update or on cancel.
- Tests: `test/update_server_299_test.dart` (exists, 4/4: popup words). Add: server newer -> download -> ready;
  sha mismatch -> deleted -> phones tried; server 204 -> phones; gate once per day; in a call -> nothing.

## Q. QR pictures get the recovery-card look (My QR + Group invite only)

1. `qr_share_card_299.dart` (prototype DONE, 1080x1350, zxing decodes at 30-100 % size and after JPEG 60).
2. `directory/presentation/my_qr_screen.dart:571` `_shareCardAsImage`: draw `QrShareCard299` (own photo if loaded,
   else first letter; name; @username; date) instead of the screen capture. `text:` = only the contact code (the QR payload).
3. `groups/presentation/screens/group_qr_screens.dart:142` `_share`: card with group photo/letter, name, members,
   "Expires in 24 hours". `text:` = only `code.encode()` (`llgq1:...`).
4. The temp PNG is deleted after the share sheet returns. No test locks the old share texts (checked).
- Tests: `test/qr_card_299_test.dart` (exists 3/3) + share text = only the code (both screens, fake share).

## R. Phone-share page redesign (user: the screenshot = this screen)

1. `share_app/data/apk_share_page.dart` only (prototype DONE): the same look as the server page (navy top with the rings logo,
   one card with the "Shared by ..." pill + Download + Urdu, step cards, note), light/dark, Noto Naskh for Urdu. Same words,
   the same 4 steps, `href="/LinkUp.apk"`, the busy page in the same look.
- Tests: `bundle210a_server_test`, `bundle210b_qr_page_test` pass unchanged (22/22).

## 299 build steps (after "Build 299")
1. Backend: write the missing parts (N.1/3/4, S, A, B.2/3, C), `gofmt`, `go vet ./...`, `go test ./...` (all pass, including
   `TestAdminWebOnlyCallsRegisteredEndpoints`), backend zip + `APPLY_backend_299.md` (restart, Windows firewall prompt once,
   router steps, phone checks). `git add internal/httpapi internal/mdns299 internal/store cmd/server`.
2. App: C + Q wiring + R, locks + full suite + analyze, bump 3.0.96+167, app zip + `APPLY_app_299.md`.

---

# PART 4: Phone checks to put in the APPLY docs

- 298: full screen map in/out + Back; every search pill while typing; incoming call crown on the Realme/ZTE; small map
  both sides + pinch + Reset + Chats scroll; way icon + toast; call bubbles + call back; clock card 3 s; crown tip 7 s;
  recovery card in WhatsApp (caption = only the code).
- 299: type `linkup.local` and `linkup.local/download` on each phone (Android version noted); `192.168.18.27/download`;
  the server window shows `MDNS_299 answering` and no `PORT80_299 off`; upload an APK on the web (owner), wrong APK refused;
  download + install from the page; an old phone gets the server popup; My QR / group QR picture + caption in WhatsApp;
  the share page from a phone.

# PART 5: Still needed from the user

1. "Build" for 298, then for 299.
2. 298-I: the fallback text (default: only the code).
3. Server window lines with `duration=` after opening the admin panel (to confirm S).
4. Router brand (for the `linkup` name steps).

Not part of 298/299 (still open in `PENDING.md`): 281 map hold, phone tests 278-296, 297 CI, release build error, etc.
