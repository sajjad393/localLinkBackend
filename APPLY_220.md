# APPLY 220 – every admin action goes to all phones (app 3.0.60+131 + backend)

**Order (after 218 and 219):**
1. Backend 218 zip, then commit and deploy.
2. App 218 zip, then commit and build the APK.
3. Backend 219 zip, then commit and deploy.
4. **Backend 220 zip**, then commit, `go test ./...` and deploy (migration 59).
5. **App 220 zip**, then commit and build the APK.

Old phones (before build 131) keep working; they just never answer. The list shows **Pending · old app**.

## What the admin sees
- **Web → Admin actions** (mock W-2):
  - Pending / Done tabs.
  - Each row shows: the action, the person or phone, "x of y" phones, Pending · old app, Done date + "via Wi-Fi Direct" etc., and a **Cancel** button.
  - The page refreshes quietly every poll. The 8 old action toasts now say "Sent to all phones · Pending until each phone does it".
- **App → Admin → Block users card → Admin actions** (new last row, mock A-4):
  - When there is no server, an orange bar: "No server · requests go by Wi-Fi Direct / mesh".
  - **Pending N / Done N** tabs.
  - Tags: Pending · Old app · "1 of 2" · Done 9 Nov · Reached N (version rule) · Cancelled.
  - Tap a pending row to see **Cancel this action?**
  - Works offline. Online it also shows the server list (web-made actions).
- The existing admin screens send a card when the server is not connected:
  - Block / Unblock (Block users).
  - Users → Status (Off = disabled, Block = suspended, Active).
  - Users → Wi-Fi rule; Users → Sign out all phones.
  - Devices → Wi-Fi rule and Remove phone.
  - Sessions → Sign out.
  - The snack says "Sent to all phones · Pending until it is done", or why it couldn't be sent (e.g. "Finish or cancel some actions first").

## What the target person sees
- **Off:** read-only (same as a block) and the red banner says "Account closed by admin. You can read old chats but can't send or call." Active again removes it.
- **Sign out / Remove phone:** the phone first sends its signed "done", then after 2 s goes to the welcome screen. Chats stay on the phone.
  - A note on the welcome screen says "Signed out by admin…" or "This phone was removed by admin…". It goes away when they tap Log in / Create account.
- **Wi-Fi rule / admin crown / old app rule / block:** applied as before (212 / 213 / 214 paths).

## How it works (short)
- **The card:** each action is a signed card (server key, or the admin phone's 212 admin cert).
  - It goes to **every** linked phone (friends or not) and the server.
  - Phones pass it on one step at a time over router Wi-Fi, Wi-Fi Direct and mesh (Kotlin `MeshOneStep` `admin_act_digest / cards / done`).
- **Only the target can read it:** the target part is locked for the target phone only (llaa1 box: X25519 + HKDF + AES-GCM).
  - The app's box is byte-for-byte the same as the server's (test with a vector made by the Go code).
- **Done:** the target does it once and sends a "done" signed with its own key. Any phone carries it back.
- **The stop:** a server note, or enough checked "done" receipts.
  - **The admin's own phone (offline) also signs a stop note with its admin cert**, so phones that can't check the receipt stop too.
  - Phones then keep only `{id, state, proof}`. Anyone still carrying the card gets told to stop (no collecting again).
- **No time limit.** Pending until done or cancelled. Caps:
  - 1,000 live cards, 5,000 finished ids;
  - 50 pending per admin.
- **Never on yourself:** a card signed on a phone of the same person is answered `refused:self` and not done.

## Small changes from TODO_220 (on purpose)
1. **App DB stays 34** (TODO said 35). Four old tests pin version 34. The two new tables are made with `CREATE TABLE IF NOT EXISTS` when the service starts (`ensureAdminActionTables()` in `local_store.dart`, new methods only).
2. **No edit to `reliable_messaging_service.dart`.** The server hint `admin_action` is heard on `socket.messages` (same as the 214 role service).
3. **No edit to `network_policy_sync_service.dart`.** The Wi-Fi doer in `app_dependencies.dart` calls `store.applyAdminNetworkPolicy` + `WifiRadioControlService().enforce` directly.
4. **Extra files TODO didn't list:**
   - `lib/app/local_link_app.dart`: 4 lines, so an admin sign-out leaves the chats at once (same path as Profile → Sign out).
   - `admin_blocked_banner.dart`: the closed text.
5. **New idea, the admin-phone stop note:** without it, a phone that never saw the target's profile would carry the card forever.
6. **A fresh digest on every new meeting** (`peer_connected`), not only every 30 s.
7. **Header in the app:** the 36 px bar slot shows the crown button with "Admin · Owner" on long press, instead of the mock's "Owner" pill (it didn't fit the existing bar).
8. **The Admin actions entry** is a row in the Block users card (it works offline). The tool menus are locked offline.

## Good to know
- **Roles:**
  - Block / Unblock / Off / Active need `manage_users` (owner).
  - Wi-Fi rule needs `manage_network_policy` (an operator can, but can't block).
  - Sign out needs `manage_sessions`; Remove phone needs `manage_devices`.
  - Crown and old-app rule are owner only.
- **Offline admin phone:** it knows only each person's latest phone (from their signed profile), so the card has boxes for that phone. When the card reaches the server, the server adds a companion card for their other phones ("1 of 2").
- **Effects of offline-made cards:** they happen on the phone, but the server's own record doesn't change until you do it online. Examples:
  - An offline Wi-Fi rule lasts until that phone next gets the server's rule.
  - Offline Off doesn't change the user status on the web.
- **What can't be cancelled:**
  - Version rule rows (409 `use_app_version_page`): change them on the App version page.
  - An action already done on a phone stays done.
- The 50-pending cap is not applied to web-made actions. An online app admin gets no action id back.
- `admin_panel.go`: setting Active from **disabled** now also unblocks (before, only from suspended).
- **Extra backend files:** `api.go`, `admin.go`, `adminweb/components.js`, `styles.css`, `app.js` (SENT_220 toasts).

## Files – app
- New:
  - `lib/features/admin/data/actions/`: `admin_action_card.dart`, `admin_action_box.dart`, `admin_action_store.dart`, `admin_action_service.dart`, `admin_action_doers.dart`, `admin_action_book.dart`, `admin_action_server.dart`, `admin_action_offline.dart`.
  - `lib/features/admin/presentation/screens/admin_actions_screen.dart`.
- Changed:
  - `lib/app/app_dependencies.dart`, `lib/app/local_link_app.dart`.
  - `lib/core/services/local_store.dart` (new methods only), `lib/core/widgets/admin_blocked_banner.dart`.
  - `lib/features/authentication/presentation/screens/welcome_screen.dart`.
  - `lib/features/admin/presentation/screens/admin_panel_screen.dart` (1 row), `admin_users_screen.dart`, `admin_devices_screen.dart`, `admin_sessions_screen.dart`, `widgets/admin_block_confirm.dart` (one offline call each).
  - `android/.../mesh/MeshBlockPolicy.kt`.
  - `pubspec.yaml`, `lib/core/config/app_build.dart` (3.0.60+131).
- Tests:
  - New: `test/bundle220a_test.dart` (9), `bundle220b_test.dart` (13), `bundle220c_test.dart` (9), `test/support/b220_fakes.dart`, `android/app/src/test/.../Admin220Test.kt`.
  - **Old test changed:** `test/nearby_190_test.dart`, the version line only (3.0.59+130 → 3.0.60+131).

## Files – backend
- New:
  - `internal/httpapi/admin_actions_220.go`, `admin_actions_box.go`, `admin_actions_220_test.go` (10 tests).
- Changed:
  - `internal/store/store.go` (migration 59: `admin_actions`, `admin_action_receipts`, `device_signing_keys`).
  - `admin_panel.go`, `admin.go`, `admin_blocks.go`, `network_policy.go`, `user_admins.go`, `version_rule.go`, `api.go`, `cmd/server/main.go`.
  - `adminweb/views.js`, `components.js`, `styles.css`, `app.js`.

## Checked here
- Backend: `go test ./...` passes; gofmt clean.
- App:
  - `flutter analyze lib`: no errors and no new warnings.
  - New tests: 31, all pass.
  - Full suite in chunks: see the chat message.
  - The Admin actions screen was rendered and compared with mock A-4.
- **Not checked here:**
  - The Kotlin compile (the toolchain was wiped; the change copies the 214 lines exactly).
  - A real phone (no APK build in this sandbox).

## Phone tests (TODO_220 §8)
1. **220-1 Server on:** web blocks Sumaira; her ZTE does it, and the web row turns Done "via server".
2. **220-2 Server on:** web Off for Zeeshan while his phone is off. Pending, no time limit. Turn the phone on: Done.
3. **220-3 Server off:** Sajjad (owner, app) blocks a 3rd phone that is **not his friend**, on the same router. App list shows Done. Later, with the server on, the web list shows it ("made offline").
4. **220-4** The same over Wi-Fi Direct, then through a middle phone (mesh).
5. **220-5** Wi-Fi rule offline: applied on the ZTE (check Settings).
6. **220-6** Sign out the ZTE offline: welcome screen with the note; chats are still there after signing in.
7. **220-7** Cancel a pending action before the phone is reached: it never happens there.
8. **220-8** An old phone (3.0.59): web shows "Pending · old app".
