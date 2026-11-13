# TODO 291 - web "Request from phones" 409 (no keys) + web Map hides a phone  [BUILT -> app 3.0.92+163 + backend web 291 (zip carries 290 too); see APPLY_app_3.0.92.md / APPLY_backend_291.md]

Owner (13 Nov, screenshots image-10/11/12 + server log): web Map shows 2 of 3 server phones, chat request
"sajjad & sumaira" -> `409 {"error":"no phone of this chat can be reached (no keys yet)"}`. "keep freeze on".
Base: app `b1a4bbe` (3.0.90) + unpushed 290 zips; backend `6e3427c` + unpushed 290 web zip.

## A. Phones never send their lock key to the server (P1, VERIFIED in code)
What happens
- Web "Request from phones" -> `AdminChatRequestCreate` -> `makeChatCard` (`admin_chat_221.go:119`) boxes the
  card for every phone of both people with `identityKeyFor(device)` (`admin_actions_220.go:133`: newest row of
  `device_identity_keys`, else `devices.identity_public_key`). No key on any phone -> 409 "no keys yet".
- Keys reach the server only from: recovery (`recovery.go:347`), transfer (`transfer.go:263`), offline join
  (`join_relay_222.go:294`), reset (`recovery_reset_222.go:133`), manual rotate (`crypto_migration.go`), or
  `PUT /api/v1/device-key` (`api.go:392`).
- Sign-up / sign-in (`auth.go:157`, `auth.go:386`) make the device row WITHOUT a key, and the app has not
  called `PUT /api/v1/device-key` since commit `38cc872` ("arina update", 14 Oct): `putIdentityKey()` and its
  call in `ReliableMessagingService.synchronize` were removed, nothing replaced them (grep: no `device-key'`
  PUT in lib/).
- Proof in data: `data/locallink.backup.db` (13 Nov, read only): 4 devices, 1 with a key (noshad's phone).
  sajjad `android-5bb3...` = no key.
What else this breaks (same cause)
- Server admin cards (`makeServerAction`, `admin_actions_220.go:197`: block/unblock, version rule, ...) skip
  phones with no key and save the card as "done" with 0 phones (silent).
- Get file / Delete completely (`admin_chat_221.go:882/1029`) -> same 409.
- 258 "server vouches for a new key" (`directory_repository.dart:1365 _serverVouches258`) is always false.
Fix (app only, smallest)
- A-1 `LocalLinkApi.putIdentityKey(publicKey)` back: `PUT /api/v1/device-key` `{"public_key": ...}` (route
  exists, `main.go:107`; first key wins, same key = 200, other key = 409).
- A-2 Call it with `crypto.publicKey()` in the start-up server check (same place as trust / admin-badges /
  admin-blocks sync, `app_dependencies.dart`) and after sign-in / sign-up / account switch. Once per app run
  after a 200 (or 10 min retry on a network error). Log `IDENTITY_KEY_PUT ok|bound|fail`.
- A-3 409 "already bound" = log once, stop for this run. NO automatic rotate (would let a stolen token swap
  the key). Silent: no pop-up (263).
- Backend: no change needed. The admin card opener already uses `crypto.sealKeyPairs()` (`app_dependencies.dart:884`),
  so the key it uploads is the one that opens the box.
- Tests (app): fake http: PUT body + token header; once per run after 200; 409 -> no loop; network error ->
  retried later; source check that start-up and sign-in call it. Old tests unchanged.
After the update: each phone sends its key on the next start with the server reachable; then
the web request reaches it. Phones still on the old app stay "no keys yet".
Owner asked "Why phone didn't send its key automatically": it did until 14 Oct. `38cc872` replaced the old
message sync (which also sent the key every 10 min) and the key upload went with it. Phone-to-phone locking
kept working (the key travels in the profile card), so nobody noticed. A-1..A-3 make it automatic again,
silent, no user step.
Web text (backend, proposed): 409 -> "These phones have not sent their lock key yet. They send it by themselves
when they open the new LinkUp and reach the server. Try again after that." 

## B. Web Map: a phone sits on top of another (P2, VERIFIED in code)
- `views.js` map `mount` (Bundle 274 W-2) keeps each old phone where it was and only new/online-changed phones
  take the `layout216` spot. `layout216` spreads phones 120 px apart BEFORE the kept spots are put back, so a
  new phone can land on a kept one. The phone drawn later covers the other.
- Screenshots: image-10 sajjad on the left, ayesha missing; image-11 "A" on the left, sajjad missing (node order
  changed between frames). The gold crown on "A" is sajjad's crown sticking out from under it: the side list
  shows ayesha with NO crown.
- Fix B-1 (web): after the kept spots are applied, move only the NEW phones (kept ones fixed) until every phone is
  >= 120 px from the others (same rule as `layout216`), stay inside the stage.
- Fix B-2 (web): one fixed order for nodes and the Phones list (server phones first, then name), so the list does
  not jump on every live frame.
- Test (backend go source check + node smoke like 290): 2 kept + 1 new near them -> min distance >= 110.
Owner v2: "don't overlap lines and users on map, both web and app" + fixed order (option 1).
- B-3 (web lines): no line runs under another phone's photo: if a server arc passes within 34 px of a third
  phone, bow it the other way, else use a smaller bow (0.60 -> 0.35); two lines never lie on top of each other
  (midpoints >= 16 px apart, else the second one bows more).
- B-4 (app, admin map + full-screen map + Chats "My links" card, all use `LinkPathsCanvas`):
  - after `_placements`, a keep-apart pass: photos (+ name tag) never overlap: min gap = 2 x photo radius + 18,
    self and server stay fixed, stays inside the map (today the server fan is ~50 px apart and edge clamps can
    stack phones on the border).
  - same line rules as B-3 for `_aerial` (line not under a third photo, no two lines on top of each other).
  - fixed order = sorted by id (already), unchanged.
  - Old test change needed: `link_paths_zoom_222_test.dart:99` sha256 of `link_paths_map.dart` below
    "Bundle 217: when the map lines may move" (placement + lines live there) -> new hash. Colours, dashes,
    motion, reduce-motion rules stay the same; only spots and bows change.
- Tests: app widget/unit: 3, 6, 12 server phones + mixed tags on 320 dp and full screen: every photo pair
  >= min gap, every line keeps 1 photo radius off third photos, no two line midpoints < 16 px. Web node smoke the same.

## C. Checked, not bugs (no change)
- Console "Content Security Policy blocks eval" + MetaMask lines: from the MetaMask browser extension
  (contentscript.js / inpage.js). Our admin JS has no eval / new Function / string timers (grep).
- `POST /api/v1/admin/device-session 403` lines: `not_admin` for normal-user phones checking at start. Expected.
- "Reconnecting..." in image-10, "Live" in image-11: the live socket closes on every page change and comes back
  after 1 s (`app.js live274`); log `/admin/live` durations match page switching. NOT VERIFIED as a bug.

## Scope / version (to confirm)
- App: `locallink_api.dart`, `app_dependencies.dart` (+ sign-in place), new test. Backend web: `views.js`
  (+ optional 409 text in `admin_chat_221.go`), new test.
- Owner: push 290 as it is; 291 = app 3.0.92+163 + backend 291 (web + 409 text) on top of 290.
