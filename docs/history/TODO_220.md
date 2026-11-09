# TODO 220 — Every admin action goes to all phones (Pending → Done)

Status: PLAN (complete, code-checked 7 Nov 2026). Freeze ON. Build only after "go ahead 220", and after 218 + 219 are pushed.
Split: **220 = the delivery system + the simple admin actions** · **221 = chats from phones + server postman + soft delete** (`TODO_221.md`).
Why 2 bundles: the server must keep storing chats until phones can answer "Get chat" (221). So the server clean-up ships only in 221, together with Get chat.
Version: app **3.0.60+131**, backend migration **59**. Reference: `ANALYSIS_220.md` (every file:line fact).

---

## 0. Your decisions that apply to 220

| # | Decision |
|---|---|
| D3 | Every admin action goes to **all phones**, friends or not (server, Wi-Fi Direct, mesh, hotspot, router). |
| D4 | The **target** phone does it and sends a signed **Done** back. |
| D5 | Done = stop: all phones stop passing it on, nobody does it again. |
| D6 | **No time limit**: Pending until Done; admin can **Cancel**. |
| Q1 | Block: Done when the blocked person's own phones confirm. Old-app Warn / Stop: "Reached N phones". |
| Q2 | **Off** while offline: read-only + "Account closed by admin". |
| Q3 | **Sign out / Remove phone** while offline: the phone signs out when the card arrives; chats stay; sign in again only with the server. |
| Q6 | Other phones can't see who an action is for · 2 phones → "1 of 2" · "Pending · old app" · server-only actions Done at once. |
| — | Must not affect other code: additions only; 212 / 213 / 214 cards keep working exactly as today. |

## 1. What 220 delivers (user view)

**Admin (web + app):**
- New page **Admin actions**: every action with **Pending · since 7 Nov**, **1 of 2 phones**, **Pending · old app**, **Done 9 Nov · via Wi-Fi Direct / server**, **Reached 14 phones**, **Done at once**, **Cancelled**. Tabs All / Pending / Done. **Cancel** on pending rows. (mock `mock_220_web02.png`, `mock_220_app04.png`)
- Actions that go to phones this way: **Block, Unblock, Off, Active again, Wi-Fi rule (person / phone), Sign out (session), Remove phone, Make / remove admin (crown), Old app Warn / Stop**.
- Offline app admin (owner / allowed role) can do: Block, Unblock, Off, Active, Wi-Fi rule, Sign out, Remove phone. (Make admin stays web Owner only, as today.)

**Target user:** Block / Off → read-only (Off says "Account closed by admin"); Wi-Fi rule applied; Sign out → sign-in screen, chats stay. No other change.

**Everyone else:** nothing visible. Their phone silently carries cards (≈ 1 KB each).

## 2. The delivery system (design)

### 2.1 Card
```
SignedBlob { body, sig }   (same type as 212: app admin_block_record.dart SignedBlob, server trust.go signedBlob)
body = {
  "v":1, "kind":"admin_action", "id":"<uuid>", "action":"block|unblock|off|active|wifi_rule|sign_out|remove_phone|admin_badge|version_rule|cancel",
  "seq":<int>, "at":"<RFC3339>", "by":"<admin name>", "signer":"server|admin", "cert":<AdminCert SignedBlob, only when signer=admin>,
  "need":<number of target phones>, "reply_key":"<admin X25519 public key, base64>",
  "to":[ {"k":"<box id>","box":"<locked details for one target phone>"} , ... ],
  "inner": <212 / 213 / 214 SignedBlob, for block/unblock/admin_badge/version_rule>,   // public, as today
  "cancels":"<action id>"   // only for action=cancel
}
```
- **Box** (only the target phone can open): `{"device":"<id>","user":"<id>","detail":{...}}` locked with format **llaa1** = ephemeral X25519 + HKDF-SHA256(info "llaa1|box") + AES-256-GCM (12-byte nonce). Key = target phone's identity key: server `device_identity_keys.public_key` (latest not retired, `store.go:215`); app offline `directory_profiles.identity_public_key` (`local_store.dart:572`).
- Other phones see only the id, the action word and the count, not who (Q6-2). Exception: block / badge / version carry their **public** 212/213/214 card inside, which is already public today.
- Size cap 16 KB per card (same as 212 `checkAdminBlock`).

### 2.2 Who may sign
- `signer:"server"`: web panel actions (server key, `trust.go:102 serverSign`).
- `signer:"admin"`: app admin offline, with the server-signed `AdminCert` (212, `admin_block_record.dart:44`); cert not ended; role must allow the action:
  block / unblock / off / active → `manage_users`; wifi_rule → `manage_users` (person) / `manage_devices` (phone); sign_out → `manage_sessions`; remove_phone → `manage_devices`; cancel → same as the action it cancels.
- Never on the admin's own account / phone (like `checkAdminBlock` "self").

### 2.3 Spreading to all phones
**Phone to phone (new, same pattern as 212 `admin_block_service.dart:470-630`):**
- `admin_act_digest` `{h, items:[[id, state]]}`: state = `p` pending, `d` done, `x` cancelled. Sent on every new link and every 60 s (min 30 s per peer). Only to linked phones (`linkedPeers`, friend or stranger relay link).
- `admin_act_cards` `{cards:[SignedBlob…], done:[receipt…]}` frames of 20.
- `admin_act_done` `{receipts:[…]}`: the stop messages.
- Receiver: check signature → keep → pass on to its other linked phones (except the sender).
- **Android**: add the 3 names to `MeshOneStep` (`MeshBlockPolicy.kt:10-36`) and to `isOneStep`. Without this a stranger link drops them. No other Kotlin change.

**Server:**
- WebSocket `{"type":"admin_action"}` (a hint) to every connected phone when a card or Done is new → phone calls sync at once.
- `POST /api/v1/admin-actions/sync` (normal device sign-in): phone sends the cards + receipts it carries (and its signing key, §2.5); gets back every card it misses + every receipt (stop) + cancels.
- `POST /api/v1/admin-actions/mine` (blocked / Off / signed-out phone, device id + token without the status check, like `AdminBlocksMine` `admin_blocks.go:236`): sends its receipts, gets only cards boxed for it.
- Phones call sync 6 s after start, on every WebSocket connect, on the hint, and every 10 min (`AdminBlockRules.serverEvery` style).

### 2.4 Target does it → Done
- A phone that can open a box (device id = mine) is a target. Checks (§2.2) → does it (§3) → makes a **receipt**:
  `SignedBlob{ body:{"v":1,"kind":"admin_action_done","action_id","device","user","result":"ok|already|refused:<why>","at","build"}, sig: Ed25519 by this phone's signing key }`.
- Saves the receipt, sends it to linked phones + server.
- The target applies a card **once** (tracked by id). A card that comes again is answered with the saved receipt.

### 2.5 Checking receipts (no fakes)
- App: verify with the target's `directory_profiles.signing_public_key`; when the key is unknown, the receipt is **carried but not counted** (the admin's phone or the server counts it).
- Server: a receipt uploaded by the target phone itself = trusted by device sign-in; a carried receipt = verified with the stored key in new table `device_signing_keys` (filled when a phone syncs; first key per device kept, a different key later is ignored and logged `SIGNING_KEY_CHANGED`).

### 2.6 Stop, tombstones, Pending
- Card state on every phone: pending → done when receipts = `need` (each counted target phone once) → **stop**: no more spreading; the card body is dropped; only `{id, state, at}` is kept (tombstone).
- Block / version actions: the inner 212 / 213 card keeps spreading by **its own old system** (every phone must keep enforcing it). Only the new card stops.
- Version rule: no single target (`need` = 0) → never "done"; shows "Reached N phones" from receipts (each phone sends a receipt once).
- **Cancel**: a `cancel` card (same signer rules). Phones that have not applied it skip it (`refused:cancelled`); those that applied it already keep their receipt (`already done`). Undo is a new opposite action (for example Unblock), not Cancel.
- Caps per phone: 1,000 live cards + 5,000 tombstones (≈ 200 KB). Over the cap: oldest **done** tombstones go first; pending never dropped. Per admin: 50 pending actions (server returns 429 `too_many_pending`, app shows "Finish or cancel some actions first").

### 2.7 Old phones
- Phones < 3.0.60 don't know the new frames (they don't pass them on). They still get blocks / version / crown by the old 212 / 213 / 214 cards, as today.
- Admin list: **Pending · old app** when the server's `device_builds` (213, `store.go:1247`) says build < 131 for a target phone, or (offline) `versionRules.peerBuilds`.

### 2.8 Multi-phone people and offline-made cards
- Web: server knows every phone of the person → one box per active phone → `need` = count.
- Offline app admin knows only the phone(s) in its directory → boxes for those. When the card reaches the server, the server makes a **companion card** (server-signed, `parent` = original id) for the person's other phones, and the admin list shows the total ("1 of 2").

## 3. Each action (what the target phone does)

| Action | Made from | Target phones | Target does (existing public code, no change) | Done |
|---|---|---|---|---|
| block / unblock | web status Suspended / Active; app Block screen | the person's phones | `AdminBlockService.instance.offer(inner)` (`admin_block_service.dart:313`) → existing read-only path (`AdminBlockBook.selfBlocked`) | each phone |
| off | web status Off; app (new "Close account" row in the user sheet) | the person's phones | `offer(inner)` with a 212 **block** card (server makes it for Off too, §4.3) + saves flag `admin_closed_v1` → banner says "Account closed by admin" | each phone |
| active (from Off) | web status Active | the person's phones | `offer(inner)` 212 **unblock** + clears the flag | each phone |
| wifi_rule | web / app Wi-Fi rule dialog | the person's phones or one phone | `NetworkPolicySyncService.applyFromCard(policy)` (**new 3-line public wrapper** over private `_apply`, `network_policy_sync_service.dart:106-117`) | each phone |
| sign_out | web / app Sessions › Sign out | that phone | sends Done first, then `AuthenticationRepository.signOut()` (`authentication_repository.dart:336`, `setSignedOut(true)`); chats stay | that phone |
| remove_phone | web / app Devices › Remove | that phone | same as sign_out + shows "This phone was removed by admin" on the sign-in screen | that phone |
| admin_badge | web Owner make / change / remove admin | the person's phones | `AdminBadgeService.instance.offer(inner)` (`admin_badge_service.dart:132`) | each phone |
| version_rule | web / app Old app rule | every phone | `VersionRuleService.instance.offer(inner)` (`version_rule_service.dart:272`) | Reached N phones |
| server-only: rename phone, settings, backup, web sessions, recovery | web / app | none | — | **Done at once** (listed for completeness, no card) |

## 4. Backend (bundle 220)

### 4.1 Migration 59 (`internal/store/store.go`, `orderedMigrationSpecs`, after 58 at `:1300`)
```
admin_actions(id PK, action, body, sig, signer, made_by, made_at, need INT, done INT DEFAULT 0, state TEXT DEFAULT 'pending',  -- pending|done|cancelled
              parent TEXT DEFAULT '', target_label TEXT DEFAULT '', targets_json TEXT DEFAULT '[]', finished_at TEXT, cancelled_at TEXT)
admin_action_receipts(action_id, device_id, result, at, body, sig, via TEXT, PRIMARY KEY(action_id, device_id))
device_signing_keys(device_id PK, signing_key, first_seen_at)
indexes: admin_actions(state, made_at), admin_action_receipts(action_id)
```
`target_label` (for example "Ali" / "Infinix (Ali)") is only in the server table for the admin list, never in the card.

### 4.2 New files
- `internal/httpapi/admin_actions_220.go`: types; `makeServerAction(action, targets, detail, inner)`; `checkAction(blob)` (signer, cert, role, self, size); `boxFor(deviceID, detail)` (llaa1); `applyReceipt(blob, via)`; `finishIfDone(id)`; companion cards (§2.8); handlers below.
- `internal/httpapi/admin_actions_box.go`: llaa1 lock (Go `crypto/ecdh` X25519 + HKDF built on `crypto/hmac` + `crypto/aes` GCM; Go 1.22, **no new module**).
- `internal/httpapi/admin_actions_220_test.go`, `admin_actions_box_test.go` (+ a test vector shared with the app).

### 4.3 Endpoints (new, `cmd/server/main.go` routes)
| Route | Who | Does |
|---|---|---|
| `GET /api/v1/admin/actions?state=&cursor=` | any admin (read) | list with label, state, done/need, old-app phones, via, made by |
| `POST /api/v1/admin/actions/{id}/cancel` | same role as the action | cancel card + audit `admin_action_cancel` |
| `POST /api/v1/admin-actions/sync` | phone | in: cards, receipts, signing key; out: missing cards, receipts, cancels |
| `POST /api/v1/admin-actions/mine` | blocked / closed / signed-out phone | its own boxed cards; takes its receipts |

### 4.4 Existing handlers: one added call each (after their current work, no change to their JSON)
- `AdminSetUserStatus` (`admin_panel.go:189`): suspended → card `block`; active from suspended → `unblock`; disabled → **server also makes a 212 block card** (`serverAdminBlock(id,"block")`) + card `off`; active **from disabled** → 212 unblock + card `active`.
  ⚠ One condition changes: today `else if status == "active" && before == "suspended"` (`:223`) → `before == "suspended" || before == "disabled"`. Without it an Off → Active person stays read-only offline. (You'll be told in APPLY; old tests checked.)
- `AdminUpdateUserNetworkPolicy` / `AdminUpdateDeviceNetworkPolicy` (`network_policy.go:61, 110`): card `wifi_rule` with the effective policy (`effectiveNetworkPolicy`).
- `AdminRevokeSession` (`admin_panel.go:286`): card `sign_out` for that session's phone.
- `AdminRevokeDevice` (`admin.go:406`): card `remove_phone`.
- `AdminSetUserAdmin` (`user_admins.go:178`) / `removeLinkedAdmin`: card `admin_badge` with the 214 card inside.
- `AdminSetVersionRule` (`version_rule.go:293`): card `version_rule` (need 0).
- `AdminBlocksSync` (`admin_blocks.go:199`): when a phone uploads an **app-made** 212 block, the server also makes the tracking card if the phone didn't (old app admins).
- WebSocket hint: `a.hub.SendTo(id, {"type":"admin_action"})` for all `a.hub.DeviceIDs()` (like `notifyAdminRole`, `user_admins.go:132`).

### 4.5 Audit
`admin_action_made`, `admin_action_done` (per phone, via), `admin_action_finished`, `admin_action_cancel`, `admin_action_offline` (cards made offline, logged when they reach the server, with the admin name from the cert).

### 4.6 Web panel (`internal/httpapi/adminweb/`)
- `views.js`: new view `admin-actions` (Safety group, after Conversations), badge with pending count; table (mock web02) → cards on phone width (217 rule); shimmer (219); Cancel with confirm "Cancel this action? Phones that already did it keep it."
- `app.js`: nav entry, route, 30 s refresh while open.
- `components.js`: status pill variants (pending / parts / old app / done / reached / cancelled).
- `styles.css`: "Bundle 220" block at the end.
- Existing Users / Devices / Sessions pages: after an action, toast "Sent to all phones · see Admin actions".

## 5. App (bundle 220)

### 5.1 DB 34 → 35 (`core/storage/database_config.dart`, upgrade in `local_store.dart`)
```
admin_actions(id PK, action, body, sig, state, need INT, done INT, mine INT,   -- mine = I made it
              applied INT DEFAULT 0, label TEXT DEFAULT '', made_at, updated_at)
admin_action_receipts(action_id, device_id, body, sig, counted INT, at, PRIMARY KEY(action_id, device_id))
```
(221 adds its own tables in DB 36.)

### 5.2 New files (`lib/features/admin/data/actions/`)
- `admin_action_card.dart`: body model, parse, `AdminActionRules` (types, caps, timings, role map).
- `admin_action_box.dart`: llaa1 lock / open (`cryptography` X25519 + Hkdf + AesGcm).
- `admin_action_store.dart`: table access, caps, tombstones.
- `admin_action_service.dart`: start / dispose, digest, frames, spread, server sync + mine, hint handling, receipts, counting, stop, cancel, `make(...)` for app admins, logs `ADMIN_ACT_*`.
- `admin_action_doers.dart`: the table in §3 (calls only existing public services).
- `admin_action_book.dart`: ChangeNotifier for screens (pending count, list).

### 5.3 Wiring (additions only)
- `app/app_dependencies.dart`: `_buildAdminActions()` like `_buildAdminBlocks` (`:840`), same `plain.send` + `linkedPeers`; start in both `initializeLocalOnlyServices` (`:925` area) and configured services.
- `core/services/reliable_messaging_service.dart`: socket `type == 'admin_action'` → `AdminActionService.instance?.syncSoon()` (next to `delivery_ack`, `:2140`).
- `features/connectivity/data/services/network_policy_sync_service.dart`: `Future<void> applyFromCard(Map<String,dynamic> p) => _apply(p);` (one public line).
- `core/widgets/admin_blocked_banner.dart`: when flag `admin_closed_v1` is set, text "Account closed by admin" instead of the block text (same banner).
- Sign-in screen (`features/authentication/presentation/screens/welcome_screen.dart`): one line showing "This phone was removed by admin" when flag `admin_removed_v1` is set.
- `features/admin/presentation/screens/admin_panel_screen.dart`: new row **Admin actions** (pending badge).
- Existing app admin actions (Block screen, user status, Wi-Fi rule dialog, Sessions, Devices): after today's call, also `AdminActionService.make(...)` when **offline**; online, the server makes the card (no double card: the server returns `action_id`).
- `core/config/app_build.dart`: 131 / '3.0.60'.

### 5.4 New screen
- `features/admin/presentation/screens/admin_actions_screen.dart` (mock app04): tabs Pending / Done, rows, tap → sheet with details (targets "1 of 2", via, made by, receipts) + Cancel. Uses `admin_theme.dart` / `admin_components.dart` (not changed).

### 5.5 Android
- `android/.../mesh/MeshBlockPolicy.kt`: `ADMIN_ACT_DIGEST`, `ADMIN_ACT_CARDS`, `ADMIN_ACT_DONE` + set + `isOneStep`. Nothing else.

## 6. Existing files touched (all additions, except the one ⚠ condition)
Backend: `store.go` (migration 59) · `main.go` (4 routes) · `admin_panel.go` (status ⚠, revoke session) · `network_policy.go` (2 calls) · `admin.go` (revoke device) · `user_admins.go` (badge) · `version_rule.go` (rule) · `admin_blocks.go` (app-made block) · web `views.js`, `app.js`, `components.js`, `styles.css`.
App: `database_config.dart` · `local_store.dart` (DB 35 tables only) · `app_dependencies.dart` · `reliable_messaging_service.dart` (1 case) · `network_policy_sync_service.dart` (1 public line) · `admin_blocked_banner.dart` (closed text) · `welcome_screen.dart` (removed note) · `admin_panel_screen.dart` (1 row) · 4 admin action screens (1 call each when offline) · `app_build.dart` · `MeshBlockPolicy.kt`.
Not touched: `admin_block_service.dart`, `admin_badge_service.dart`, `version_rule_service.dart`, `admin_theme.dart`, `admin_components.dart`, `admin_shell_screen.dart`, `admin_dashboard_screen.dart`, mesh routing, chat code.

## 7. Tests
**Backend** (`admin_actions_220_test.go`, `admin_actions_box_test.go`):
1. Server card signed + verifies; changed body refused. 2. Admin cert card: role map (operator can block, auditor can't), ended cert refused, self refused.
3. Box: only the target key opens; shared test vector opens in Dart too.
4. Status Suspended / Active / Off / Active-from-Off make the right cards (and the 212 card for Off); old status JSON unchanged.
5. Wi-Fi rule, revoke session, revoke device, badge, version rule each make one card; their old JSON unchanged.
6. Sync: upload card from a phone (offline admin) → stored + companion card for the other phone; download missing cards; receipts counted once; carried receipt verified with `device_signing_keys`; fake receipt refused.
7. `mine`: blocked phone gets only its boxed cards. 8. Cancel → state cancelled; later receipts kept as "already".
9. Done when receipts = need; version rule counts "reached". 10. 50 pending cap → 429. 11. Old-app flag from `device_builds`.
12. All existing tests (212, 213, 214, 218, 219) green unchanged.

**App** (`bundle220a..e_test.dart`):
1. Card parse / sign / verify; box lock / open; test vector. 2. Spread over a fake stranger link: digest → cards → passes on; tombstone stops repeats.
3. Target doers: block → `selfBlocked` true; off → banner "Account closed by admin"; active → cleared; wifi → `applyFromCard` called; sign_out → Done sent before `signOut()`; badge / version → `offer()` called.
4. Receipts: counted only with a known key; fake refused; "1 of 2". 5. Cancel before / after apply.
6. Caps: 1,000 / 5,000; pending never dropped. 7. Server sync + `mine` with a fake server. 8. Admin actions screen renders all states (mock match).
9. Old tests (212 `admin_block_*`, 213 `version_*`, 214 `admin_badge_*`, `MeshBlockPolicy` Kotlin tests) unchanged and green. 10. Full suite.
**Kotlin:** existing `MeshBlockPolicyTest` + 3 new asserts (new names are one-step).

## 8. Phone tests (Realme = Sajjad, ZTE = Sumaira, + a 3rd phone if possible)
- 220-1 Web Block Sumaira (ZTE online) → Admin actions "Done · via server" within seconds.
- 220-2 ZTE Wi-Fi + data off → web Block → Pending → ZTE meets Realme by Wi-Fi Direct (Realme online first) → Done "via Wi-Fi Direct".
- 220-3 Server off: Sajjad (owner, app) blocks a 3rd phone that is **not his friend**, on the same router → Done in the app list; later server on → web list shows it (made by Sajjad, offline).
- 220-4 Off → ZTE "Account closed by admin" (read-only); Active → normal.
- 220-5 Wi-Fi rule offline → applied on ZTE (check Settings).
- 220-6 Sign out ZTE while offline → sign-in screen, chats still there after sign in.
- 220-7 Cancel a Pending action → "Cancelled"; ZTE never applies it.
- 220-8 Old phone (3.0.59) as target → "Pending · old app".
- 220-9 Sumaira logged in on 2 phones → "1 of 2" then "Done".
- 220-10 Normal chat, calls, Wi-Fi Direct, mesh unchanged (quick check).

## 9. Delivery
- Backend zip first (migration 59 + web page), deploy; then app zip (3.0.60+131).
- Commits: `v3 bundle 220 backend: admin actions go to all phones (signed cards, done receipts, admin actions page, off and sign out offline)` · `v3 3.0.60+131 bundle 220: admin actions go to all phones (cards over every link, target does it and replies done, pending list, cancel)`.
- APPLY_220.md lists the ⚠ status condition change and every touched file.
