# TODO 222 - account recovery fix, forgot password, map zoom, crown room, offline accounts for the admin, nearby phones carry the account

Status: **PLAN ONLY (freeze on).** Nothing is built until you say "go ahead 222".
Base: app `0c2e3e8` (220) + bundle 221 (built, NOT pushed yet). Backend `7d92d82` + 221.
**221 must be pushed first**: 222 needs 221's server box key (`answerBoxKey`) and migration 60.
Version after 222: **3.0.62+133**. Server migration: **61**.

Mocks: `mock_222_all.jpg` (all 13 pictures in one file). Single files: `mock_222_r1..r5.png`, `mock_222_m1.png`, `mock_222_m2.png`, `mock_222_c1.png`, `mock_222_u1..u3.png`, `mock_222_w1.png`, `mock_222_w2.png`.

---

## 0. Rules for this bundle

- **Map lines do not change.** They stay the 217 lines: curved arc, moving dashes plus a glow dot, colour by link (green = Server, amber = Wi-Fi Direct, purple = Mesh, teal = Local Wi-Fi). Blocked = grey, dashed, no motion. No motion when "Remove animations" or battery saver is on. 222 only changes **zoom** (5) and **which nodes the small map draws** (6). It does not touch line code (`_ArcPainter`, dash speed, colours, glow). A test checks that the line painter's settings are the same as in 217.
- Your choices (6 Nov):
  - Code + new password works at once, with no admin.
  - Your other phones stay signed in.
  - No code → request to the admin → one-time code.
  - Offline reset only on the same phone with its saved code; the server is updated later.
  - The admin sees offline accounts in Users.
  - Nearby phones carry the account to the server.
- Standing rules:
  - Touch only the files in the list below; if more are needed, I tell you.
  - Old tests stay unchanged. If one must change, I only add setup or an expectation, and I tell you.
  - The build matches the mocks 100%.
  - English + Urdu strings.
  - No time limit on waiting: Pending until it is done.
- Not in 222: block fix, video, Wi-Fi call gaps, other `PENDING.md` items.

---

## 1. "Invalid recovery request data" (the request never reaches the admin)

**What you saw:** Account recovery → Send → red "invalid recovery request data" (`uploads/Screenshot_20261106-043713.png`).

**Cause (checked in code):**
- App `features/recovery/bloc/account_recovery_bloc.dart:130` sends `username` and `password: ''`, with no `recovery_code`.
- Server `internal/httpapi/recovery.go:74` (`CreateAccountRecovery`) refuses when the password AND the recovery code are both empty → 400 `invalid recovery request data`.
  - Nothing is saved in `account_recovery_requests`, so the web and app Admin → Recovery lists stay empty.
- The old screen's field "Recovery code - Enter the one-time code" means the admin's one-time code (after approval). It is not the user's own recovery code, which confuses people.
- Today's recovery flow is "move my account to this phone": `CompleteAccountRecovery` (`recovery.go:210`) signs out **all other phones** (`revokeOtherDevicesAndMigrateGroups`, `recovery.go:565`). It never sets a new password.

**Fix:** replaced by the clear flows in 2, 3 and 4 below. The old "move my account to this phone" path (password or code + lost phone) stays as it is and is still reachable from the same screen ("I lost my phone" link). No old test changes.

---

## 2. Forgot password, user HAS the recovery code (online)

Mocks: R-1 (login link), R-2 (form), R-4 (new code shown once).

**App:**
- `setup_screen.dart`, login mode, under the Password field (~line 362): new text button **"Forgot password?"** → opens the new screen.
  - The old "Account recovery" text button (~402) is renamed "I lost my phone" and stays.
- New screen in `account_recovery_screen.dart` (new widget `ForgotPasswordView222`; the same file keeps the old view):
  - Fields: Username, Recovery code (format `XXXX-XXXX-XXXX`, spaces and dashes allowed), New password, New password again. The eye button shows or hides the password.
  - Checks before sending: same as register (min length, same twice, no control characters).
  - Top status chip: green "Server online", or orange "No server · works on this phone only" (then section 4 applies).
  - Button **"Set new password"**.
  - Link at the bottom: **"I don't have my recovery code"** → section 3.
- `account_recovery_bloc.dart`: new events `ForgotPasswordSubmitted222(username, code, newPassword)` and states `resetting` / `resetDone(newCode)` / `resetFailed(reason)`.
- `recovery_repository.dart` + `locallink_api.dart` (~862): new `resetPasswordWithCode(...)` → `POST /api/v1/account/recovery/reset`.
- On success:
  - Save the session like a normal login (same code as `authentication_repository.login`, not a new path).
  - Save the NEW recovery code into `localRecoveryCode` (`local_store.dart` ~1415).
  - Show R-4 "Your new recovery code" **once**, with Copy and "I saved it". Then go to Chats.
- Error texts:
  - Wrong code or name: "Username or recovery code is wrong".
  - 429: "Too many tries. Try again in 1 minute".
  - Account blocked or not active: "This account is not active. Ask the admin".

**Server (backend):**
- New `POST /api/v1/account/recovery/reset` in new file `internal/httpapi/recovery_reset_222.go`; route in `cmd/server/main.go` next to line 68, with `WithIdempotency("account-recovery-reset")`.
- Request: `{username, recovery_code, new_password, device_id, device_name, platform, identity_public_key}`. The body limit is 12 KB, the same as `CreateAccountRecovery`.
- Checks:
  - `validUsername`; code ≤128; `new_password` with the same rules as register (`maxPasswordLen`, `hasControlChars`).
  - Rate limit `allowPersistentSecurityAction("recovery-reset", username, 5, time.Minute)` → 429 + `Retry-After: 60`.
  - Unknown user → dummy PBKDF2 (the same timing guard as `recovery.go:96`) → 401.
  - `recovery_code_enabled=0` → 401 (same text).
- Steps, in one transaction:
  1. Compare `tokenHash(code)` with `users.recovery_code_hash` (`secureTokenEqual`).
  2. Make a new salt and set `password_hash` with `derivePasswordKey(new, salt, passwordIterations)`.
  3. Sign in this phone: the same device/session insert as `LoginAccount` (`auth.go:169`). **Other phones are NOT revoked** (your choice).
  4. Make a new recovery code: store its hash and return the plain code once.
  5. Audit `password_reset_with_code` (user, device) + `auditSecurity`.
- Response: the same shape as `AuthResponse` (`auth.go:37`): `{account, device, token, expires_at, recovery_code}`, so the app reuses its login parser.

**Logs (app):** `RESET_CODE_SENT`, `RESET_CODE_OK`, `RESET_CODE_FAIL reason=<401|429|403|net>`. The code and passwords are never logged.

---

## 3. Forgot password, NO recovery code (online)

Mocks: R-5 (waiting card on the phone), W-2 (web Recovery list).

**App:**
- "I don't have my recovery code" → username only → **"Ask the admin"**.
- Waiting card R-5:
  - Text: "Sent to the admin · waiting" (clock, never red; no time limit, Pending until done).
  - Button "Check now"; checked every 30 s while the screen is open.
  - The request id + secret are kept in secure storage, so it survives an app restart.
- After the admin approves, the card asks for the **one-time code** (given in person) + new password twice → "Set new password" → signed in → R-4 shows a new recovery code once.
- In the app, Admin → Recovery shows the request with the orange tag **"No code"** and buttons Approve / Reject. The one-time code then shows big with Copy.

**Server:**
- `CreateAccountRecovery` (`recovery.go:48`): a new optional field `no_code: true` lets username-only requests through when both the password and the code are empty.
  - The row is saved with new column `kind='no_code'`; old rows default to `'move'`.
  - Unknown user → the same "sent" answer (no hint whether the name exists), but nothing is saved.
  - Rate limit as today: 6 per minute per name.
- Migration 61: `ALTER TABLE account_recovery_requests ADD COLUMN kind TEXT NOT NULL DEFAULT 'move'`.
- `AdminRecoveryRequests` (`recovery.go:375`) returns `kind`. The web list (`adminweb` Recovery page) shows the tags: **No code** (orange), **Reset with code** (green, audit only, no buttons), and the old ones as now.
- `CompleteAccountRecovery` (`recovery.go:210`): a new optional field `new_password`.
  - If `kind='no_code'`, `new_password` is required. It sets the password, makes a new recovery code (returned once), and **keeps the other phones signed in**.
  - The old `'move'` requests behave exactly as today (they still sign out other phones).
- The reset with code (section 2) also writes a `kind='reset_code'`, `status='completed'` row, so the Owner sees it in W-2.

**Logs (app):** `RECOVERY_NOCODE_SENT id=…`, `RECOVERY_NOCODE_APPROVED`, `RECOVERY_NOCODE_DONE`.

---

## 4. Offline (no server), same phone

Mock: R-3.

**Today:**
- The phone already accepts its saved recovery code in place of the password at login (`authentication_bloc.dart:159`, `localRecoveryCode`).
- Nothing on screen says so, and there is no way to set a new password.

**New:**
- The same Forgot password screen with the orange chip "No server · works on this phone only".
- Allowed only if **all** of these hold:
  - username = the last account on this phone (`local_store.dart` `username`);
  - the code matches `localRecoveryCode`;
  - this phone has that account saved.
  - Otherwise: "Without the server this works only for the last account on this phone".
- On success:
  - Make a new local salt + PBKDF2 hash (`core/crypto/password_kdf.dart`, 180000 rounds, the same as the server) and sign in locally (offline login path, 214).
  - Keep "new password waiting for server" in secure storage `pwd_reset_pending_222 = {salt, hash, made_at, code_hash}`. The plain password is never stored.
- When the server is back (`offline_account_sync_service.dart`, the same trigger as the 218 sync):
  - Call `/api/v1/account/recovery/reset` with the saved code and the new password hash. A new server field `new_password_hash` + `new_password_salt` + `iterations` is accepted ONLY together with a valid recovery code.
  - On OK: clear the pending entry, save the new code, and show R-4 once ("Your password is now on the server. New recovery code").
  - On 401 (the code was changed on the server meanwhile): banner "Your new password is only on this phone. Use Forgot password again when online". No loop.
- Not allowed offline: the no-code request (it needs the admin's server).

**Logs:** `RESET_OFFLINE_OK`, `RESET_SYNC_SENT`, `RESET_SYNC_OK`, `RESET_SYNC_FAIL reason=…`.

---

## 5. Admin map: zoom out to see the whole map

Mock: M-2.

**Cause:**
- `link_paths_full_screen.dart`: InteractiveViewer `minScale: 1` (~433), and `_setScale` (~122) clamps to 1.0.
- The canvas is bigger than the screen, so you can never see it all.

**Fix (zoom only, lines unchanged):**
- `fitScale = min(view.w / canvas.w, view.h / canvas.h)`, capped at 1. Recalculated on rotate or resize.
- `minScale = fitScale`; the "−" button and pinch both go down to it. At fit, "−" turns grey and a small tag **"Whole map"** shows for 1.5 s.
- Double tap: fit ↔ 2x. The "◎" button centres on You at the current zoom (as now).
- At fit, name pills and crowns keep their own size rules from 217 (no change), and lines keep the same width and motion.
- Opening the map starts at 1x centred on You (as now). The last zoom isn't saved.

**Test:** `link_paths_zoom_222_test.dart`:
- min scale = fit; "−" is disabled at fit; double tap toggles.
- The arc painter settings are equal to 217 (dash length, speed, colours, blocked style).

---

## 6. Small map shows nobody, full map shows 1 user

Mock: M-1.

**Cause:**
- Both maps use the same node list (`admin_panel_screen.dart` `_nodesFor` 333).
- With no other phone linked, the small map (`link_paths_map.dart:233`) returns only the text "No phones linked right now" and draws neither You nor the Server.
- The full map always draws You → Server. That is the "1 user".

**Fix:**
- The small map always draws **You + Server** with the normal 217 line when the server is online (green, moving dashes), the same as the full map. Server offline = no line (as on the full map).
- "No other phones linked" becomes a small grey note on the right of the card.
- The count in the full-map header and the small-map subtitle use the same function, so they can't differ.

**Test:** `link_paths_small_222_test.dart`:
- With 0 neighbours, both maps draw the You + Server nodes.
- With N neighbours, both maps show the same count.

---

## 7. Crown overlap / clipping

Mock: C-1. Screenshots: `-021924` Profile, `-022256` Users, `-021852` Settings, `-021918` Account.

**Cause:** `core/widgets/admin_crown.dart` `AdminCrown.decorate` paints the crown above the photo (`top: 0.4 - h`) without taking layout space. Near a clip or card edge, it is cut or crosses the border:
- **Profile** `profile_screen.dart:405-420`: `PhotoTapOverlay` clips to the photo box → only the crown band shows.
- **Admin → Users** `admin_users_screen.dart` rows inside `AdminListCard216` (`admin_kit216.dart` Clip.antiAlias) → the first row's crown is cut by the card.
- **Settings** card `messenger_settings_screen.dart:213` (Clip.antiAlias) and **Account** card `account_center_screen.dart:195` → the crown crosses the card border.

**Fix:**
- New `AdminCrown.room(radius)`: the height the crown needs above a photo of that size.
- The 4 screens add that much top padding around the photo only (no other layout change). `PhotoTapOverlay` gets `clipBehavior: Clip.none` for the crown layer only; its camera overlay stays clipped to the circle.
- The crown look itself stays the 217 crown.

**Test:** `crown_room_222_test.dart` pumps every widget that shows a crown (Profile, Account, Settings, Users row, People row, chat header, map node, person sheet, web-like cards…). For each one it checks that the crown's painted box is inside its nearest clip or card box.

---

## 8. Offline accounts in Admin → Users (learned phone to phone)

Mocks: U-1 (list), U-2 (sheet), W-1 (web).

**Today:**
- Admin → Users loads only from the server (`admin_bloc.dart:162` `load()` → `repository.users`).
- An account made offline (`local_store.dart:1411` `saveOfflineAccount`, `sync_state='local_only'`, `accountId` empty) shows on the map and in People, but not in Users until HER phone reaches the server.
- With no server, Users says "needs the server" (`admin_tool_page216.dart` `needsServer`).

**How the phone decides "not on server yet" (signed card is NOT changed):**
- The signed profile card (`directory_models.dart:68` `canonicalMap`) must not get a new field, because old phones would then fail the signature check. So the decision uses only what the admin phone already knows:
  1. New setting `admin_users_cache_222`: the last server Users list (ids + usernames), saved on every successful `load()`.
  2. People in `directory_profiles` (`local_store.dart:569`) whose `user_id` is not in that cache, or whose user id is empty or equals their device id (an offline account has no server id yet; **check the exact format at build**).
  3. Plus the 218 `NotOnServer218` list and `id_moves` (`offline_account_sync_service.dart` 318-328) to remove people who have joined meanwhile.
- If the admin phone never loaded Users from the server (empty cache), only rule 2's empty/device-id case is used, so known server people are never shown wrongly.

**App UI (U-1, U-2):**
- Users = server rows + local rows.
  - Local row: grey tag **"Offline account"**; sub-line "Not on server yet · Wi-Fi Direct · 2 h ago" (last link + last seen from `presence_service.dart` / `directory_profiles.synced_at`).
- New filter chip **"Not on server"** (with count) next to the existing chips.
- No server: the "needs the server" page is replaced by the local rows + an orange bar **"No server · showing people seen nearby"**. Server-only tools stay locked as now.
- Tap a local row → sheet U-2:
  - Name, @name "(may change if taken)", phone (if in her card), seen via, last seen.
  - Status "Waiting to join the server", or "Join carried by 2 phones" (from section 9).
  - Actions: **Block** and **Sign out** by the 220 admin action cards (Pending until achieved; they work offline). No delete or rename until she joins.
- When she joins (id move seen or server list has her): the local row disappears and her normal server row shows with a small **"Joined from offline"** note for 7 days.

**Web (W-1):**
- Phones already report neighbours (216, `admin_216.go:420` `Neighbor{device_id, link}`, in memory for 4 min). New optional fields `name`, `username`, `offline` (old phones simply don't send them).
- Migration 61 adds table `seen_offline_accounts(device_id PK, username, name, phone_model, seen_by_device, link, first_seen, last_seen, join_boxes)`.
  - Kept 60 days after the last sighting; the row is removed when that device registers or joins.
- `AdminUsers` (`admin_panel.go:118`) gets `?include=not_on_server` → extra rows with `status:"not_on_server"`.
- The web Users page:
  - New chip **"Not on server N"**; grey tag "Not on server yet"; columns Seen by / Last seen.
  - The line "Join box carried by N phones · waiting for a phone with internet". No action buttons until she joins.

**Logs:** `USERS_LOCAL_ROWS n=… server=on|off`, `USERS_JOINED_FROM_OFFLINE user=…`.

---

## 9. Nearby phones carry her account to the server

Mock: U-3 (her waiting / joined card).

**Today:** only her own phone can register (`offline_account_sync_service.dart` → `/api/v1/auth/register`). Nobody carries it.

**Join box (her phone makes it once, right after the offline account is created, and again if she changes name or password before joining):**
- Contents: `{v:1, username, display_name, phone?, pwd_salt, pwd_hash, iterations:180000, recovery_hash, device_id, identity_key, signing_key, made_at}`.
  - The password hash is the same PBKDF2-SHA256 as the server (180000 rounds, 16-byte salt, 32-byte key; app `core/crypto/password_kdf.dart` = server `auth.go:445`). **The real password never travels.**
- Signed with her phone's signing key, then locked for the server with an llaa1 box (X25519 + HKDF, the same as 221 `admin_actions_box.go` `lockBox`) to the server's box key (221 `answerBoxKey()`).
  - Carriers can't open or change it.
- Limit: box ≤8 KB.

**Server box key on a phone that was never online:**
- From the phone that shared the app (210 sharer page or nearby share). **Check at build** whether the 210 share carries the server record; if not, use only the next rule.
- Or from nearby phones, but only when **2 different phones** hand the same server-signed record.
- Used ONLY for the join box. Admin trust stays as today (`admin_block_service.dart:672` `pinTrust` from the server only).
- No key yet → the box waits on her phone (her card says "Waiting for a phone that knows the server").

**Carrying:**
- New frame `account_join` over Wi-Fi Direct / mesh / router: Kotlin `MeshBlockPolicy.kt` one-step relay, the same as 220/221 frames. It does not affect other frames.
- New app file `lib/features/authentication/data/services/join_carrier_222.dart`. It keeps boxes in a new table `join_boxes_222(box_id, from_device, box, made_at, got_at, status)`.
  - Limits: ≤200 boxes per carrier; each is kept up to 60 days; no other time limit.
- Any phone with the server uploads it: new `POST /api/v1/auth/join-relay` (backend new file `internal/httpapi/join_relay_222.go`; route in `cmd/server/main.go`).
  - Request: `{box_id, box}` with the uploader's normal session; rate limit 30 per minute per uploader.

**Server (`join_relay_222.go`):**
- Open the box with `answerBoxKey`, check her signature, and check that `device_id` matches the signing key.
- Apply the **same 218 rules** as `RegisterAccount` (`auth.go:45`), reusing its code (no copy):
  - same name + same password hash → join her phone to that account;
  - name taken by someone else → auto rename (sumaira37) and tell friends (218);
  - phone number taken → local only + Audit (218).
- **Idempotent:** the same `box_id` twice → the same stored answer. New table `join_relay_answers(box_id PK, user_id, answer, made_at)`, kept 60 days.
- Answer: server-signed `{status: joined|renamed|refused, username, user_id, box_id}` plus her session token **locked for her phone** (an llaa1 box to her identity key), so only she can use it.
- Audit `account_joined_by_relay` (user, carrier device).
- Also removes her row from `seen_offline_accounts` (section 8).

**Answer back:**
- The answer travels phone to phone like 221 chat parts.
- Her phone checks the server signature, opens the token, then runs the normal "after the server said yes" steps (profile, signed card, contacts, outbox: the existing 218 path, not new code).
- Carriers delete her box when the signed "joined" note reaches them. That note also travels, so others stop carrying.
- If her own phone gets online first, the normal register runs and the carried box is answered "already joined" (idempotent).

**Her screen (U-3):**
- Before: "Sent with a nearby phone · waiting for the server" (clock, never red).
- After: "You are on the server now". If renamed: "Your name is now @sumaira37 (sumaira was taken)".

**Logs:** `JOIN_BOX_MADE`, `JOIN_BOX_CARRIED from=… n=…`, `JOIN_RELAY_UPLOADED box=…`, `JOIN_RELAY_DONE status=…`, `JOIN_ANSWER_TAKEN`. Never the hash or the token.

---

## Files

**Backend (zip 1, deploy first, migration 61):**
- `internal/httpapi/recovery.go`: no-code requests, `kind`, `new_password` on complete.
- NEW `internal/httpapi/recovery_reset_222.go`: reset with code, online + offline sync.
- NEW `internal/httpapi/join_relay_222.go`: carried join.
- `internal/httpapi/admin_216.go`: optional neighbour fields, save offline sightings.
- `internal/httpapi/admin_panel.go`: Users `include=not_on_server`.
- `internal/store/store.go`: migration 61 (`kind` column, `seen_offline_accounts`, `join_relay_answers`).
- `cmd/server/main.go`: 2 new routes.
- Admin web (`internal/httpapi/adminweb/`): Users chip/tag/columns; Recovery tags.
- Tests: `recovery_222_test.go`, `join_relay_222_test.go`, `admin_users_222_test.go`.
- `TODO_222.md`.

**App (zip 2, one commit `v3 3.0.62+133 bundle 222: …`):**
- `features/authentication/presentation/screens/setup_screen.dart`: "Forgot password?" + "I lost my phone".
- `features/recovery/presentation/screens/account_recovery_screen.dart`, `features/recovery/bloc/account_recovery_bloc.dart`, `features/recovery/data/repositories/recovery_repository.dart`.
- `core/services/locallink_api.dart`: reset, no-code, join-relay calls.
- `features/authentication/data/repositories/authentication_repository.dart`: offline reset.
- `features/authentication/data/services/offline_account_sync_service.dart`: reset sync, join box made.
- NEW `features/authentication/data/services/join_carrier_222.dart`.
- `core/services/local_store.dart`: `pwd_reset_pending_222`, `admin_users_cache_222`, `join_boxes_222` table.
- `features/admin/presentation/screens/link_paths_full_screen.dart`: zoom only.
- `features/admin/presentation/widgets/link_paths_map.dart`: small map nodes only.
- `features/admin/presentation/screens/admin_panel_screen.dart`: shared count.
- `core/widgets/admin_crown.dart` (`room()`), `profile_screen.dart`, `account_center_screen.dart`, `messenger_settings_screen.dart`, `admin_users_screen.dart`: crown room.
- `features/admin/bloc/admin_bloc.dart`, `admin_tool_page216.dart`: local rows, no-server bar.
- `features/connectivity/data/services/presence_service.dart`: neighbour report extra fields.
- Android `android/app/src/main/kotlin/com/sajjad/locallink/mesh/MeshBlockPolicy.kt`: `account_join` frame.
- l10n EN + UR strings.
- `pubspec.yaml`: 3.0.62+133.
- Tests:
  - `forgot_password_222_test.dart`, `offline_reset_222_test.dart`;
  - `link_paths_zoom_222_test.dart`, `link_paths_small_222_test.dart`;
  - `crown_room_222_test.dart`;
  - `admin_users_offline_222_test.dart`, `join_carrier_222_test.dart`.
- `TODO_222.md`, `APPLY_222.md`.

---

## Phone tests

- **222-1** Logout → Forgot password → username + code + new password (online) → signed in, new code shown once. The old password fails; the new one works on another phone; your other phone is still signed in.
- **222-2** "I don't have my recovery code" → request → web Recovery shows "No code" → Approve → one-time code → new password → signed in.
- **222-3** Server off → Forgot password on the same phone with the code → signed in. Later online → the new password works on the server, and a new code is shown once.
- **222-4** Wrong code 5 times → "Too many tries. Try again in 1 minute".
- **222-5** Admin map: "−" and pinch go down to the whole map, then "−" turns grey; double tap goes back to 2x. **Lines look and move exactly as before.**
- **222-6** Small map with nobody else linked shows You + Server with the moving green line.
- **222-7** Crown fully visible on Profile, Users, Settings and Account (light + dark).
- **222-8** Sumaira makes an account offline and meets the admin phone with no server → Admin → Users shows "Offline account · Not on server yet" + the orange no-server bar.
- **222-9** Sumaira's phone stays offline; Ali's phone meets her, then gets internet → web Users shows her (joined). The answer comes back to her through Ali → she is joined without ever being online.
- **222-10** Name taken → she becomes sumaira37 by the carried join; her friends see the new name.
- **222-11** Ali's logs and box never contain her password (search the log file for it).

---

## Mocks (in `mock_222_all.jpg`)

| Mock | Shows |
|---|---|
| R-1 | Login with "Forgot password?" |
| R-2 | Forgot password online (code + new password) |
| R-3 | Forgot password with no server (same phone) |
| R-4 | New recovery code shown once |
| R-5 | No code → sent to the admin, waiting |
| M-1 | Small map: You + Server, **217 curved moving line unchanged** |
| M-2 | Full map zoomed out to "Whole map", **lines unchanged** |
| C-1 | Crown with room on Profile / cards |
| U-1 | Admin → Users with offline accounts (no server bar) |
| U-2 | Offline account sheet |
| U-3 | Her waiting / joined card |
| W-1 | Web Users: "Not on server yet" |
| W-2 | Web Recovery: "No code" / "Reset with code" tags |
