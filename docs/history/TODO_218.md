# TODO 218 — Offline account joins the server: every case, made smooth

**Status:** plan + mocks done. The freeze is ON, so no code until "go ahead 218".

**Checked against code (2026-11-05):**
- App: `c8af499` + the 216 patch (3.0.57+128).
- Backend: `3a0fa5e` (216, GitHub `localLinkBackend`).
- Every line number below is from those files.
- 218 is built after 217. If 217 is not built yet, nothing in 218 depends on it; only the version number moves.

**Mocks (the built UI must match these):**

| Mock | Screen |
|---|---|
| `mock_218_app01.png` | U-1 Chats, the one-time "You're on the server now" card (+ orange phone line) |
| `mock_218_app02.png` | U-2 the 3 texts of that card + the recovery card (old server only) |
| `mock_218_app03.png` | U-3 the "stuck" cards (phone already on the server / pairing code) |
| `mock_218_app04.png` | U-4 Account → Profile, number used by another account |
| `mock_218_app05.png` | U-5 chat: clock bubble + Message info "Not sent yet" + grey note |
| `mock_218_app06.png` | A-5 Admin app → Users → person: "Joined from offline · date" |
| `mock_218_web01.png` | W-1 Web Users → Manage tag + Audit log line |

- All pages: `mock_218_all.jpg`.
- Mock script: `mock218_src/mk218.py`.

**Your choices (2026-11-05), all "my advice":**
1. Same name + same password → join this phone to her own account.
2. Name taken by another person → automatic rename, told to her and her friends.
3. Phone number taken → save the rest; keep the number on her phone only, with a note.
4. Sending to someone not on the server → keep it waiting (no red mark); it goes by nearby, or by the server once they join.
5. Recovery code → the server keeps her offline code.
6. Joined her own account + a different number → the account's server number wins; her offline number is used only if the account has none.
7. Stuck card shows the other @name only when the server proves it is the same phone (`hardware_fp`); otherwise "another account".
8. Zips: 217 first, then 218 (separate app zips).
9. Old red messages (failed because the person was not on the server) → tried again by themselves, from the **last 60 days**.

**Delivery:**
- **Backend zip first:** migration v58, additive JSON only, so old apps keep working.
- **Then ONE app zip with ONE commit:** `v3 3.0.59+130 bundle 218: ...` (3.0.58+129 if 217 is not built first).

---

## 0. Rules for this bundle (from your standing rules + the code check)

1. **No signature change on methods that old test fakes override.**
   - `test/bundle216f_test.dart:58` overrides `register({username, password, deviceName, pairingCode})` exactly. So `AuthenticationRepositoryContract.register/login` stay as they are.
   - The new register/login fields are read **inside `LocalLinkApi`** from `LocalStore` (§2.1).
2. **New members only** on `LocalStore`, `LocalLinkApi`, `DirectoryRepository` and `ReliableMessagingService`.
   - All test fakes that `implements` them use `noSuchMethod` or `Fake`. I checked: `g211_fakes`, `195b`, `197`, `204`, `177`, `196`, `202`, `183`, `190`, `154`, `170`, `g6`, `216f`.
   - No new abstract member on `DirectoryRepositoryContract` / `MessagingRepositoryContract`.
3. **`bundle216f_test.dart` must stay green unchanged:**
   - **Line 101:** the happy path logs exactly `CHECK, PROBE, PUBLISH, PUBLISH, DONE` through `offlineAccountLogHook216`. The new 218 lines use `AppLogger.info` directly, never `offlineAccountLog216`.
     - Exception: on the conflict path only, one `LINK_TRY` / `LINK_NO` line is fine (lines 112–115 use `contains` / `any`).
   - **Line 115:** `store.renamed == renamed`, so `writeRenamedAccountNotice` is still called on a rename.
   - **Line 138:** `GIVE_UP after=6`, so the sign-in try happens **once** (first conflict only) and does not count as one of the 6 tries.
   - **Its fake `_Auth`** has no `login()` (`noSuchMethod` throws an **Error**), so the sign-in try is `try { … } catch (e) { … }` (all throwables) → falls back to the rename.
   - **Its fake `_Store`** lacks new helpers, so every 218 step after the publish is in its own try/catch.
4. **No local DB version change** (stays 34). New small state lives in the `settings` table (`readSetting` / `writeSetting`).
5. **Old tests that change:** only `test/nearby_190_test.dart:291` (the version line, as every bundle).

---

## 1. Backend — 218a (zip 1, deploy first)

### 1.1 Migration v58 — `internal/store/store.go`
Add a spec after the v57 spec (line 1260), same style:
```sql
CREATE TABLE IF NOT EXISTS offline_ids (
  offline_id TEXT PRIMARY KEY,   -- offline_<12 hex>
  user_id    TEXT NOT NULL,
  device_id  TEXT NOT NULL,
  linked_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_offline_ids_user ON offline_ids(user_id);
CREATE INDEX IF NOT EXISTS idx_offline_ids_device ON offline_ids(device_id);
```

### 1.2 Error codes — `internal/httpapi/api.go`
- New helper next to `writeError` (line 1134): `func writeErrorCode(w, status int, code, message string)` → `{"ok":false,"error":message,"error_code":code}`.
- The message texts stay the same as today (old apps read the text).

### 1.3 Register — `internal/httpapi/auth.go:45 RegisterAccount`
1. **Input struct** gets 2 optional fields:
   - `OfflineUserID string json:"offline_user_id"`;
   - `RecoveryCode string json:"recovery_code"`.
2. **Offline id:**
   - `offline := strings.TrimSpace(in.OfflineUserID)`;
   - valid only if it matches `^offline_[0-9a-f]{12}$` (new `var offlineIDPattern`); otherwise treat it as empty (never an error).
3. **Recovery code:** `if c := normalizeRecoveryCode(in.RecoveryCode); recoveryFormat218.MatchString(c) { recoveryCode = c }`.
   - `recoveryFormat218` = `^[A-Z0-9]{5}(-[A-Z0-9]{5}){3}$`. The app and the server use the same format (`recovery_code.dart:12` / `auth.go:368`).
   - Otherwise keep `newRecoveryCode()` (line 88).
   - It is hashed exactly as today: `tokenHash(recoveryCode)`.
4. **Conflict answers** (lines 104–120):
   - users unique → `writeErrorCode(409, "USERNAME_TAKEN", "username is already registered")`;
   - devices unique → `writeErrorCode(409, "DEVICE_TAKEN", "device id is already registered")`;
   - pairing (line 76) → `writeErrorCode(403, "PAIRING_REQUIRED", "invalid pairing code")`.
5. **In the same transaction**, before commit, when `offline != ""`: `INSERT OR IGNORE INTO offline_ids(offline_id,user_id,device_id,linked_at) VALUES(?,?,?,?)`.
6. **After commit:** `if offline != "" { a.carryOfflineBlock218(userID, offline, deviceID) }` (§1.5) and `a.audit("account_from_offline", userID, "offline="+offline)`.
7. **The answer** stays the same (`recovery_code` = the code used).

### 1.4 Login — `auth.go:155 LoginAccount`
1. The input struct gets `OfflineUserID` (same check as 1.3.2).
2. Errors get codes (texts unchanged):
   - unknown user / wrong password → `401 "BAD_LOGIN"`;
   - line 255 → `409 "DEVICE_TAKEN"`;
   - line 275 → `403 "PAIRING_REQUIRED"`.
3. After a successful commit with `offline != ""`:
   - `INSERT OR IGNORE INTO offline_ids` (user = the existing account);
   - `carryOfflineBlock218(user.ID, offline, deviceID)`;
   - audit `account_link_offline`.

### 1.5 Block carry-over — `internal/httpapi/admin_blocks.go` (new func at the end)
```go
// carryOfflineBlock218: a block made while the person was an offline account
// (record on offline_x or on her phone id) follows her to the real account.
func (a *API) carryOfflineBlock218(userID, offlineID, deviceID string)
```
1. Read every `admin_blocks` row (max `maxAdminBlockRecords`). Pick the newest one (`adminBlockNewer`) where either:
   - `target_user_id == offlineID`; or
   - the body `target_devices` contains `deviceID`.
2. If there is none, or its action is `unblock` → return.
3. If the real user already has a row that is newer → return.
4. Otherwise `a.serverAdminBlock(userID, "block", "auto: blocked while offline")`. That is server-signed, includes all her phones, and sets status `suspended`.
5. `a.adminAudit("server", "admin_block_carry", userID, "from="+offlineID)`.

### 1.6 Resolve — new file `internal/httpapi/users_resolve_218.go`
- `POST /api/v1/users/resolve` (route in `cmd/server/main.go` next to line 90).
- `a.authorizeDevice(r)` is required. Rate limit: `allowSecurityAction("resolve", deviceID, 30)` per minute.
- **In:** `{"ids":[...]}`, at most 100, each ≤ 128 chars, no control chars.
- **For each id:**
  - if it matches `offlineIDPattern` → `SELECT o.user_id, u.username, o.device_id FROM offline_ids o JOIN users u ON u.id=o.user_id WHERE o.offline_id=? AND u.status IN ('active','suspended')`;
  - else → `SELECT d.user_id, u.username, d.id FROM devices d JOIN users u ON u.id=d.user_id WHERE d.id=? AND COALESCE(d.status,'active')='active'`.
- **Out:** `{"found":[{"id","user_id","username","device_id"}]}`. Unknown ids are left out.

### 1.7 Contacts — `internal/httpapi/contacts.go:75 AddAccountContact`
- Before the check at line 95: `if offlineIDPattern.MatchString(contactID) { if real := a.resolveOffline218(contactID); real != "" { contactID = real } }`.
- 404 → `writeErrorCode(404, "NOT_ON_SERVER", "user not found")`.
- The 200 answer adds `"user_id": contactID` (additive).

### 1.8 Profile — `internal/httpapi/profile.go:117 UpdateProfile`
- **Line 204** → `writeErrorCode(409, "USERNAME_TAKEN", …)`.
- **Line 215** → `writeErrorCode(409, "PHONE_TAKEN", "phone number is already registered")`. Then `a.audit("profile_phone_conflict", userID, "number=***"+last4(phoneNumber))`, where `last4` is a new tiny helper that returns the last 4 digits or "".

### 1.9 Admin lists — `admin_panel.go:10 AdminUser` + `admin_216.go:204 enrichUsers216`
- New field `JoinedOfflineAt string json:"joined_offline_at,omitempty"`.
- In `enrichUsers216`, one more chunked query: `SELECT user_id, MIN(linked_at) FROM offline_ids WHERE user_id IN (…) GROUP BY user_id`.

### 1.10 Web — `adminweb/views.js manage216()` (line 556)
- Under `@username` in the person row: `${u.joined_offline_at ? '<span class="tag218">Joined from offline · ' + ui.esc(ui.dateShort(u.joined_offline_at)) + '</span>' : ''}`.
- **CSS** (end of `styles.css`): `.tag218 { display:inline-block; margin-top:6px; padding:3px 10px; border-radius:11px; background:var(--blue-soft,#1d2a4a); color:#9DB8FF; font-size:11.5px; font-weight:500 }`.
- If `ui.dateShort` does not exist, use the date part of the existing `ui.time`-style helper (checked at build time; no new date logic).
- The Audit log page already lists the `profile_phone_conflict` / `admin_block_carry` / `account_from_offline` lines (generic list). Nothing to add.

### 1.11 Go test — new `internal/httpapi/offline_join_218_test.go` (`LOCALLINK_ENV=test`)
1. Register with `offline_user_id=offline_0123456789ab` → 201; an `offline_ids` row exists; resolve finds it with the same `user_id`.
2. Register with `recovery_code=ABCDE-FGHIJ-KLMNO-PQRST` → the answer has the same code; the recovery request with that code passes the hash check.
3. Register with a bad offline id (`offline_zz`) → 201, no row (ignored).
4. A second register with the same username → 409 + `error_code=USERNAME_TAKEN`, and the text is still "username is already registered".
5. Login with `offline_user_id` → a row for the existing user.
6. Store a server-signed block on `offline_x` with `target_devices=[dev1]` → register with `offline_x` / `dev1` → user status `suspended` and an `admin_blocks` row for the new user id with action `block`.
7. The same with an `unblock` newest → stays active.
8. Contacts add with `offline_x` (linked) → 200 + `user_id` = real; with an unknown `offline_` id → 404 `NOT_ON_SERVER`.
9. Profile with a phone already used → 409 `PHONE_TAKEN` + an audit row with `***` + 4 digits.
10. Admin users list → `joined_offline_at` set only for the linked user.
11. An old register body (no new fields) → the same answer shape as before.
12. Resolve without a token → 401. 101 ids → 400.

---

## 2. App — 218b: joining the server (`offline_account_sync_service.dart`, 281 lines)

### 2.1 Extra fields sent — `lib/core/services/locallink_api.dart`
1. **`registerAccount`** (line 315) and **`loginAccount`** (line 353): before the POST, `final extra = await _offlineJoinFields218();` and spread it into the JSON body.
2. **New private method:**
   ```dart
   Future<Map<String, String>> _offlineJoinFields218() async {
     if (!store.isOfflinePending) return const {};
     final out = <String, String>{};
     try {
       final id = (await store.profile())?.userId ?? '';
       if (id.startsWith('offline_')) out['offline_user_id'] = id;
     } catch (_) {}
     final code = store.localRecoveryCode ?? '';
     if (code.isNotEmpty) out['recovery_code'] = code;
     return out;
   }
   ```
   - Login sends only `offline_user_id`: the recovery code is not sent on a link, so her existing account keeps its own code.
3. **New `ApiError218` reading** in `_error()` (line 1165) stays the same for the text.
   - Add a new method `static String errorCode218(Object error)` that pulls `code=XYZ` out of the exception text.
   - So `throw Exception(_error(response))` becomes `throw Exception('${_error(response)}${_code(response)}')`, where `_code` returns `' [code=USERNAME_TAKEN]'` or `''`.
   - The old text checks (`isUsernameConflict`: "username" + "already registered") still match, because the text is unchanged at the start.
4. **New `Future<List<Map<String, String>>> resolveIds218(List<String> ids)`** → `POST /api/v1/users/resolve`, 6 s timeout. On any error it returns `[]` (never throws).

### 2.2 The sign-in try — `autoPublish` (line 162)
In the loop at line 196, inside `catch (error)` where `isUsernameConflict(error)` is true, and **only when `attempt == 0`**:
```dart
if (attempt == 0 && !linkTried) {
  linkTried = true;
  AppLogger.info('OFFLINE_ACCOUNT_LINK_TRY', detail: 'user=@$pendingUsername');
  try {
    final r = await _auth.login(username: pendingUsername, password: secret,
        deviceName: _deviceName(), pairingCode: _storedPairing());
    await _afterJoin218(r, kind: JoinKind218.linked, finalName: pendingUsername, secret: secret);
    ok = true;
    offlineAccountLog216('DONE', 'user=@$pendingUsername');
    return null;
  } catch (e) {
    if (_codeOf(e) == 'PAIRING_REQUIRED') { await _setBlock218('pairing'); return null; }
    AppLogger.info('OFFLINE_ACCOUNT_LINK_NO', detail: offlineReason216(e));
  }
}
// then today's rename (unchanged)
```
- This one step also fixes the lost-answer case: the first register made the account, so the retry's sign-in succeeds with the same password and the same phone id.
- **Linked:** `_afterJoin218` does **not** push the local profile (her server profile wins). It calls `getProfile(downloadAvatar: true)` through the join hook (2.6) and drops a pending local update. The phone number she typed offline is kept only if the server profile has none.

### 2.3 After every success — `publish()` (line 69) + new `_afterJoin218`
`publish()` already calls `register`, `markAccountSynced` and the id swap. Change the end of `publish()` to `await _afterJoin218(result, kind: …, finalName: username, secret: password)`, keeping the existing swap code inside it.

`_afterJoin218(AuthResult r, {required JoinKind218 kind, required String finalName, required String secret})`. Each step has its own try/catch and logs with `AppLogger.info` only:
1. **Profile row:** `seeded.copyWith(userId: r.account.id, username: r.account.username, profileVersion: seeded.profileVersion + 1, updatedAt: nowUtc)` → `saveProfile`. This fixes C6 and makes friends take the new card.
2. **Pending profile** (`store.pendingProfileUpdate()`):
   - **renamed / plain:** if one exists → write it again with `username = r.account.username`. If none exists → `setPendingProfileUpdate(seeded.displayName, r.account.username, phoneNumber: seeded.phoneNumber, phoneVisibility: …, discoverable…: …)`. If the profile has a local photo (`localAvatarPath` exists) and no pending photo → `setPendingProfileAvatarPath(copy of it)` (C7).
   - **linked:** `clearPendingProfileUpdate()`, but only if it has no phone number or the server already has one; that is checked in the hook after `getProfile`.
3. **Offline sign-in copy:** `_auth.mirrorOfflineLogin(username: r.account.username, password: secret, recoveryCode: r.recoveryCode.isNotEmpty ? r.recoveryCode : (store.localRecoveryCode ?? ''))` (C9).
4. **Recovery code:** if `kind != linked && r.recoveryCode.isNotEmpty && r.recoveryCode != store.localRecoveryCode` (an old server) → save `join_recovery_218` = the new code (the U-2 recovery card).
5. **My own id move:** append `{old: offline_x, new: r.account.id}` to the setting `id_moves_218` (JSON list, max 500).
6. **The note for Home** (setting `join_notice_218`, JSON): `{"kind":"plain|renamed|linked","name":"<final>","was":"<pending name>","at":"<iso>"}`. The phone line is added later by 2.4. Also call `writeRenamedAccountNotice(name)` on a rename, as today (216f:115).
7. **Clear the stuck flag:** `offline_publish_block_218` → `''`.
8. **The join hook:** `OfflineAccountSyncService.onJoined218?.call(kind)`. A static `Future<void> Function(JoinKind218)?`, set by `AppDependencies` (2.6).

`enum JoinKind218 { plain, renamed, linked }` goes in the same file. The rename result passes `renamed`.

### 2.4 Phone number taken — `locallink_api.dart:402 syncPendingProfile`
In the `catch` (line 436), before `return false`:
```dart
if (_isPhoneTaken(error) && pending != null && (pending['phone_number'] ?? '').toString().isNotEmpty) {
  final number = pending['phone_number'].toString();
  await store.writeSetting('phone_local_only_218', number);
  await store.setPendingProfileUpdate(pending['display_name'] ..., pending['username'] ..., phoneNumber: '', ...same flags);
  AppLogger.info('OFFLINE_ACCOUNT_PHONE_LOCAL_ONLY');
  _markJoinPhoneLine218(); // join_notice_218.phone = true (if the note exists)
  return syncPendingProfile(); // once more without the number (no loop: phone is now '')
}
if (_isUsernameTaken(error) && pending != null) {
  final current = store.username ?? '';
  if (current.isNotEmpty && current != pending['username']) { rewrite pending with username=current; return syncPendingProfile(); }
}
```
- `_isPhoneTaken` = code `PHONE_TAKEN` or the text "phone number is already registered".
- **Keeping her number on her phone:** in `account_repository.dart` `saveProfile` (line 47), `local` keeps `phoneNumber` (her card to friends uses `store.profile()`, so her number stays). Only the server copy is empty.
- **A server profile must not wipe her number:** `getProfile()` (`locallink_api.dart:441`) after `LocalProfile.fromJson`: if `phone_local_only_218` is set and the server number is empty → `result = result.copyWith(phoneNumber: localOnly)` before `store.saveProfile`.
- **Clear:** when `saveProfile` is called with a phone number different from `phone_local_only_218` → clear the setting (a normal save; if it is taken again, the same flow runs).

### 2.5 Stuck cases — no silent retry
- **New setting `offline_publish_block_218`:** `''` | `'pairing'` | `'device:@name'` | `'device'`.
- **In `autoPublish`**, right after the `CHECK` log (line 164): `if (from != 'card' && (await _readBlock218()).isNotEmpty) { AppLogger.info('OFFLINE_ACCOUNT_BLOCKED_BY'); return null; }`.
  - This uses `readSetting` (216f's `_Store` has it; it returns null → not blocked).
- **When it is set:**
  - code `DEVICE_TAKEN` (or the text "device id is already registered") in the line 206 branch → `_setBlock218(owner.isEmpty ? 'device' : 'device:@$owner')`.
  - code `PAIRING_REQUIRED` (or the text "pairing") → `_setBlock218('pairing')`.
  - The existing FAIL log line stays.
- **New public `Future<String?> tryAgainFromCard218({String? pairingCode})`:**
  - saves the pairing code into the pending payload (`store.offlinePending['pairing_code']`, through an existing or new small store setter `updateOfflinePendingPairing218`);
  - clears the block → `autoPublish(from: 'card')`.

### 2.6 The join hook — `lib/app/app_dependencies.dart`
In the constructor / `init`, next to the reconnect burst (line 1049):
```dart
OfflineAccountSyncService.onJoined218 = (kind) async {
  AdminBlockBook.instance.setSelf(userId: store.accountId ?? '', deviceId: store.deviceId ?? '');
  if (kind == JoinKind218.linked) { try { await api.getProfile(downloadAvatar: true); } catch (_) {} }
  unawaited(account.syncPendingProfile());
  unawaited(directoryRepository.publishOwnProfile());
  unawaited(directoryRepository.syncServerContacts());
  unawaited(messaging.flush());
};
```
- `dispose()` sets it back to null (only if it is still ours).
- Logs: `OFFLINE_ACCOUNT_LINKED`, `OFFLINE_ACCOUNT_PROFILE_PUSHED`, `OFFLINE_ACCOUNT_RECOVERY_KEPT|NEW`, `OFFLINE_ACCOUNT_MIRROR_OK`. No password, key or number in any log.

---

## 3. App — 218c: friends' phones follow her new id

### 3.1 `LocalStore` (new methods only; `local_store.dart`)
1. `Future<List<Map<String, dynamic>>> directoryProfilesByDevice218(String deviceId)`: all rows (today's `directoryProfileByDeviceId`, line 1696, returns only 1).
2. `Future<void> moveUserId218(String oldId, String newId)`, in one `timedTx('move_user_218')`:
   - `contacts`: if no row for newId → `UPDATE contacts SET user_id=? WHERE user_id=?`. If both exist → copy a non-empty `custom_name` / `is_private=1` onto the new row, then delete the old one.
   - `blocked_users`: `INSERT OR IGNORE (newId, blocked_at of old)` + delete old.
   - `directory_sync_state`: `UPDATE … SET peer_id=? WHERE peer_id=?` (ignore unique errors → delete old).
   - `directory_profiles`: `DELETE WHERE user_id=oldId`.
   - The settings key `server_confirmed_contacts`: drop oldId (newId gets confirmed by the next push).
   - The settings key `id_moves_218`: append `{old,new}` (max 500, the oldest dropped).
   - After the transaction: `_blockedChanges.add(null)` and the existing contacts change notifier (the same one `addContact` uses).
3. `Future<String> movedId218(String id)` → the new id if `id_moves_218` has it, else `id`.

### 3.2 The move rule — `directory_repository.dart:930 _saveVerified`
After the signature check (line 948), before `final oldRow = …` (line 950):
```dart
if (!profile.userId.startsWith('offline_')) {
  for (final row in await store.directoryProfilesByDevice218(profile.deviceId)) {
    final oldId = row['user_id']?.toString() ?? '';
    if (oldId.startsWith('offline_') && oldId != profile.userId &&
        (row['signing_public_key']?.toString() ?? '') == profile.signingPublicKey) {
      await store.moveUserId218(oldId, profile.userId);
      PeerPhotoBook.instance.moveUser218(oldId, profile.userId);
      _movePendingEdges218(oldId, profile.userId);
      AppLogger.info('ID_MOVED', detail: 'old=$oldId new=${profile.userId}');
    }
  }
}
```
- The same signing key is the proof: only her phone has that key. A different key means no move.
- **Block by phone too (C13):** at line 933, `if (await store.isBlocked(profile.userId) || await store.isBlocked(profile.deviceId)) return false;`.
  - `blockDeviceAndUser` (`local_store.dart:1894`) already writes the phone id into `blocked_users`.
- `_movePendingEdges218`: in `_pendingEdges` (line 75), replace `from_user_id` / `to_user_id` = old with new, then save as the code at line 465 does.
- **`PeerPhotoBook.moveUser218(old,new)`** (`peer_photo_book.dart:107`, map `_users`): new method that re-keys the entries whose value or key is old.

### 3.3 Server help — `directory_repository.dart:1351 syncServerContacts`
At the start (after the `accountId` check):
1. `ids` = local contact ids starting with `offline_` (`store.rawContactUserIds()`) + the phone ids of chats whose directory row has an `offline_` user id. At most 100, every 30 min (memory time `_lastResolve218`) or right after the join hook.
2. `final found = await api.resolveIds218(ids)`.
3. For each one with `user_id`: `store.moveUserId218(<old offline id of that phone>, user_id)`.
4. **The push loop** (line 1359): skip ids that still start with `offline_`. Log `CONTACT_PUSH_SKIP_OFFLINE` once per id per app run (no warning every 2 min).
5. **On `NOT_ON_SERVER`** from `addAccountContact`: keep it local, no warning.
- Also used by 218d: phone ids returned by resolve are taken out of `NotOnServer218` (4.1).

---

## 4. App — 218d: messages to someone not on the server

### 4.1 New `lib/features/messaging/data/services/not_on_server_218.dart`
```dart
class NotOnServer218 extends ChangeNotifier {
  static final instance = NotOnServer218._();
  final _since = <String, int>{};           // phone id -> ms
  static const ttl = Duration(minutes: 30);
  bool has(String phoneId);                  // false after ttl (and removes it)
  void add(String phoneId);  void remove(String phoneId);
  @visibleForTesting void resetForTest();
}
```

### 4.2 `reliable_messaging_service.dart`
1. **The error handler** (line 1931): take `RECIPIENT_NOT_FOUND` and "recipient device not found" **out** of `permanent`.
   - **New branch before it:**
     ```dart
     final notOnServer = errorCode == 'RECIPIENT_NOT_FOUND' || errorText.contains('recipient device not found');
     if (notOnServer) {
       final row = await store.outboxItem(id);
       final peer = row?['recipient_id']?.toString() ?? '';
       if (peer.isNotEmpty) NotOnServer218.instance.add(peer);
       final cur = row?['status']?.toString() ?? '';
       if (cur != 'sending' && cur != 'direct_delivered' && cur != 'delivered') {
         await store.updateOutboxStatus(id, 'queued');
       }
       AppLogger.info('MESSAGE_WAIT_NOT_ON_SERVER', detail: 'id=$id');
       return;
     }
     ```
   - The message row keeps its status (`queued` / `sending` = the clock); it is never set to `failed`.
   - The other permanent errors are unchanged.
2. **`flush()`** (line 1320), after `canUseDirect` is known: if `!canUseDirect && NotOnServer218.instance.has(recipientId)` → `continue`.
   - This means no attachment upload and no server send (C17).
   - Log `MESSAGE_SKIP_NOT_ON_SERVER` once per id per run.
3. **`_maybeOnlineBackup`** (line 2984): first line `if (NotOnServer218.instance.has(item['recipient_id']?.toString() ?? '')) return;`.
4. **Wake-up:** `NotOnServer218.instance.addListener` (in the service constructor) → on a `remove` → `unawaited(flush())`.
   - `remove` is called from 3.2 (when a card with a real id arrives for that phone), from 3.3 (resolve found it) and on a reconnect (`_fireSilentResyncBurst`, `app_dependencies.dart:1049`, clears entries older than 5 min).
5. **One-time clean-up** of messages that failed before 218 (setting `rnf_requeue_218`):
   - on the first `synchronize()` after the update, read outbox rows with status `failed` from the last 60 days that have **no attachments pending upload error** and whose recipient phone id is either:
     - unknown to `resolveIds218` → set `queued` + `NotOnServer218.add` (the clock shows; they wait);
     - or now known → set `queued` (they go now).
   - Message rows go back to `queued` (the clock). Then write the flag.
   - It runs at most once. A row that fails again for another reason is marked failed again, as today.
6. **Groups: no change.** Server groups fan out on the server (`send_group_message`, `group_messaging_service.dart:1100`), and members who are not on the server get the nearby copy through the offline group path (G-series), as today.

### 4.3 Message info — `message_options.dart:267 showMessageInfoSheet` + `chat_screen.dart:567 _showInfo`
1. **New optional parameter** `String notOnServerName = ''` (new optional only; callers unchanged).
2. **1:1:** if `notSentYet` → the existing `info_not_sent` row ("Not sent yet", `Icons.schedule_rounded`) instead of `info_sent`. Today that is only for groups (line 312).
3. If `notOnServerName.isNotEmpty` → under the row, a grey box `Container(key: ValueKey('info_not_on_server'), padding: 14×12, radius 12, color: colors.surfaceContainerHighest)` with the text `'$name is not on the server yet. It goes when $name is nearby or joins the server.'` (13.5, `onSurfaceVariant`).
4. **`chat_screen._showInfo`:**
   - `notSentYet: status == 'queued' || status == 'pending' || status == 'sending'`;
   - `notOnServerName: (notSentYet && NotOnServer218.instance.has(widget.controller.device.id)) ? _peerName(widget.controller.device) : ''`.
5. **The bubble does not change** (the same clock `message_bubble.dart:336`).

---

## 5. App — 218e: what she sees (match the mocks)

New file `lib/features/home/presentation/widgets/join_cards218.dart`.

### 5.1 `JoinCard218` (shared look; U-1 / U-2 / U-3)
- **Margin** `EdgeInsets.fromLTRB(16, 12, 16, 4)`. **Padding** `fromLTRB(16, 16, 16, 14)`. **Radius** 16.
- **Colour** light `#FFFFFF`, border 1 px `#EAECF0` + shadow (`0 2 6`, `#101828` at 9 %); dark `colorScheme.surfaceContainerHigh`, border `outlineVariant`, no shadow.
- **Row:** a 36 px icon circle + gap 12 + a column:
  - title 15 / w600 / `onSurface`;
  - 6 px gap; body 13 / h 1.38 / `onSurfaceVariant`;
  - optional orange line 12.5 `#B54708` (dark `#FDB022`), 6 px above;
  - optional field (`LocalLinkTextField`, 44 tall, 12 above);
  - buttons row right-aligned, 12 above, gap 8.
- **Buttons:** `TextButton` (13.5 / w600, primary) and a tonal pill (`FilledButton.tonal`, height 36, radius 18, `#E6EDFF` / primary text; dark `primaryContainer`).
- **Icons:**

| Kind | Icon | Colour on background (light) |
|---|---|---|
| ok | `Icons.cloud_done_outlined` | `#079455` on `#E7F7EE` |
| phone | `Icons.phone_android_outlined` | `#B54708` on `#FEF6E7` |
| warn | `Icons.warning_amber_rounded` | same as phone |
| key | `Icons.key_outlined` | primary on `#EEF3FF` |

  Dark: the same icon colours on the colour at 16 % opacity.

### 5.2 Texts (exact)

| Card | Title | Body | Buttons | Key |
|---|---|---|---|---|
| Done · plain | You're on the server now | Your account is on the server now. Your chats stay on this phone. | Account · OK | `join218_done` |
| Done · renamed | same | @{was} was taken, so you are @{name}. Your chats stay on this phone. | same | same |
| Done · linked | same | This phone joined your account @{name}. Your chats stay on this phone. | same | same |
| + phone line | — | orange: Your phone number stays on this phone only. | — | `join218_phone_line` |
| Stuck · device | Can't join the server yet | This phone is already linked to @{owner} on the server. Ask the admin to remove this phone on the web (Devices), then tap Try again. (No owner known → "This phone is already linked to another account on the server. …") | Try again | `join218_stuck_device` |
| Stuck · pairing | The server needs a pairing code | Ask the admin for the code. + field "Pairing code" (hint "Enter the code") | Try again | `join218_stuck_pairing` |
| Recovery (old server only) | Your recovery code changed | Write it down. The old one no longer works. + read-only field "Recovery code" = code | Copy · I saved it | `join218_recovery` |

Button keys: `join218_account`, `join218_ok`, `join218_try`, `join218_copy`, `join218_saved`. The pairing field key is `join218_pair_field`.

### 5.3 Where — `home_screen.dart`
1. **`initState`** (line 371 block): read `join_notice_218`, `join_recovery_218` and `offline_publish_block_218` into state fields `_join218`, `_recovery218`, `_block218`.
   - If `_join218` is null but the old renamed notice exists (`readRenamedAccountNotice`) → make `_join218 = {kind: renamed, name: renamed, was: ''}`. With `was` empty, the text is "You are @{name} on the server now. Your chats stay on this phone."
   - Keep `clearRenamedAccountNotice()` as today.
2. **The Chats list** (line 1359): replace the `_renamedAccountNotice` `_SyncNotice` with:
   - `if (_join218 != null) JoinCard218.done(...)`;
   - `if (_recovery218 != null) JoinCard218.recovery(...)`.
   - The `_renamedAccountNotice` field stays (now unused for drawing). Keep the literal `_renamedAccountNotice` in the file in case old source scans read it; I found none in tests, but it costs nothing.
3. **Stuck card:** `if (widget.store.isOfflinePending && _block218.isNotEmpty) JoinCard218.stuck(...)` **instead of** `_offlineAccountBanner()` (line 1368; the banner shows when there is no block, as today).
   - **Try again** → `_offlineSync?.tryAgainFromCard218(pairingCode: field.text)`, a busy spinner in the button, then re-read the 3 settings.
4. **OK** → delete `join_notice_218` and hide it. **Account** → the existing `_openAccount`-style navigation to the Profile page, then hide it. **I saved it** → delete `join_recovery_218`. **Copy** → `Clipboard.setData` + snack "Copied".
5. **Live update:** the join can finish while Home is open (reconnect burst), so the join hook (2.6) also fires `JoinNotice218.changed` (a static `ValueNotifier<int>`). Home listens and re-reads the settings.

### 5.4 Profile note — `profile_screen.dart:443`
- **On load:** `_phoneLocalOnly = (await store.readSetting('phone_local_only_218')) == phone.text.trim()` (the screen already has the store through its deps; if not, through `widget.accountRepository`'s new `Future<String?> phoneLocalOnly218()`).
- **The phone field** gets `decoration: InputDecoration(enabledBorder: OutlineInputBorder(borderRadius: 12, borderSide: BorderSide(color: Color(0xFFF79009), width: 1.6)), labelStyle: TextStyle(color: Color(0xFFB54708)))` only when `_phoneLocalOnly`.
- **Under it** (key `profile_phone_local_note`): `Row([Icon(Icons.error_outline_rounded, 14, #B54708), SizedBox(6), Text('Used by another account. Saved on this phone only.', 12, #B54708)])`, with 6 px top padding.
- **Typing** a different number hides the note at once (`onChanged`).

### 5.5 Admin app — `admin_users_screen.dart` sheet (line 225)
- Under the `@user · phones · online` text: `if ((u['joined_offline_at'] ?? '').toString().isNotEmpty)` → `Padding(top: 4, Row([Icon(Icons.cloud_outlined, 13, c.blue), SizedBox(5), Text('Joined from offline · ${dateShort216(u['joined_offline_at'])}', style: t.custom(12, FontWeight.w500, c.blue))]))`.
- Key `a218_joined_offline`.
- Use the existing 216 date helper (`age216` sits next to it; use the same file's date format; checked at build time, no new format).

---

## 6. Tests to add (old tests unchanged except the version line)

**`test/bundle218b_test.dart`** (fakes like 216f: `_Store implements LocalStore` + `noSuchMethod`, `_Auth implements AuthenticationRepositoryContract` with `register` + `login`):
1. Name free → `join_notice_218.kind == plain`; the profile row id = the real id, the username is the final one, `profileVersion + 1`; the mirror was called; `id_moves_218` has `{offline, real}`.
2. Name taken + login 200 → `kind == linked`; the register calls are `['sumaira']` only; no rename; `GIVE_UP` not logged.
3. Name taken + login throws `BAD_LOGIN` → today's rename; `kind == renamed`; `store.renamed` set.
4. Lost answer: the fake register throws once after "creating" → the 2nd run gets `USERNAME_TAKEN` → login 200 → linked, no orphan rename.
5. Pending profile with the old name + rename → the pending username is rewritten. With no pending → one is created from the profile.
6. `DEVICE_TAKEN` → block `device`; the next `autoPublish()` logs `OFFLINE_ACCOUNT_BLOCKED_BY` through AppLogger and makes no register call; `tryAgainFromCard218()` calls register.
7. `PAIRING_REQUIRED` → block `pairing`; `tryAgainFromCard218(pairingCode: '1234')` sends '1234'.
8. An old server's recovery code differs → `join_recovery_218` is saved; the same code → not saved.
9. The 216f happy-path log hook steps are still exactly `CHECK, PROBE, PUBLISH, PUBLISH, DONE` (the 218 steps don't use the hook).

**`test/bundle218b_api_test.dart`** (`MockClient` for `LocalLinkApi`):
- `registerAccount` sends `offline_user_id` + `recovery_code` only when pending;
- `loginAccount` sends `offline_user_id` only;
- `syncPendingProfile`: `PHONE_TAKEN` → a second PUT with `phone_number: ''`, `phone_local_only_218` set, returns true;
- `USERNAME_TAKEN` → a PUT with `store.username`;
- `getProfile` with an empty server phone + the local-only flag → the local number is kept.

**`test/bundle218c_test.dart`** (real `LocalStore` on sqflite ffi, as the 195 tests do):
- `moveUserId218` moves the contact with `custom_name` + `is_private`, the block and the sync state, and deletes the old directory row;
- `_saveVerified` with the same phone + same key + an `offline_` row → moved; a different key → 2 rows, no move;
- a card whose phone id is blocked → refused;
- `syncServerContacts` with a resolve hit → moved + the real id pushed; with no hit → `addAccountContact` not called for `offline_` ids.

**`test/bundle218d_test.dart`** (the `rms` fake socket from the 205 / 203 tests):
- error `RECIPIENT_NOT_FOUND` → outbox `queued`, message not `failed`, `NotOnServer218.has(peer)`;
- an outbox row `sending` (nearby) + `RECIPIENT_NOT_FOUND` → stays `sending`;
- `flush` with no nearby route + a not-on-server peer → no `send_message`, no upload call;
- `_maybeOnlineBackup` skipped;
- `NotOnServer218.remove` → flush runs and sends;
- the one-time requeue runs once (flag); a 59-day-old failed row goes back to `queued`, a 61-day-old one stays `failed`;
- other permanent errors are still `failed`.

**`test/bundle218e_test.dart`** (widgets):
- the Home done-card texts for plain / renamed / linked + the phone line; OK clears the setting;
- the stuck device / pairing card + Try again calls `tryAgainFromCard218`;
- the old renamed notice alone → the renamed card;
- Profile note shown / hidden by typing;
- Message info: `info_not_sent` for a 1:1 queued message + `info_not_on_server` text;
- the admin sheet line `a218_joined_offline` only when the field is set.

**Reset hooks in `tearDown`:**
- `NotOnServer218.instance.resetForTest()`;
- `OfflineAccountSyncService.resetForTest()` (also sets `onJoined218 = null`);
- `JoinNotice218.changed.value = 0`.

**Backend:** §1.11.

---

## 7. Acceptance (built vs mock, 100 %)

| Mock | Check |
|---|---|
| U-1 | The card sits first under the top bar on Chats, 16 px sides, icon circle 36, title 15 w600, body 13, orange line 12.5, Account (text) + OK (tonal) on the right; the Conversations label follows. |
| U-2 | The 3 body texts match word for word; the recovery card has a read-only field + Copy / I saved it. |
| U-3 | The stuck card replaces "Account saved — it will publish itself…" while blocked; Try again (tonal); the pairing field 44 tall. |
| U-4 | The orange phone border 1.6 + the label colour; one line under it with a 14 px icon; nothing else on the page changes. |
| U-5 | The bubble is the same clock; Info shows "Not sent yet" + the grey box text exactly. |
| A-5 | One blue line under the subtitle, 12 w500, cloud icon 13. |
| W-1 | The tag under @name (11.5, pill, blue soft); Audit lines exist. |

The UI check picture `ui_check_218.jpg` shows U-1, U-3, U-4, U-5 and A-5 built (`RepaintBoundary` + `toImage`), the same way as 216j.

---

## 8. Phone tests for you (after the backend deploy + APK)

| # | Do | Expected |
|---|---|---|
| 218-1 | New offline account on the ZTE, chat with Sajjad by Wi-Fi Direct, then let the ZTE reach the server | U-1 card; same chats; the web shows her real name + photo; Sajjad's phone shows ONE Sumaira (friend, same name) |
| 218-2 | Offline account with a name that someone else has | Renamed card; friends see the new @name within a minute of meeting |
| 218-3 | Offline account with the same name + password as an existing account | Linked card; the web shows 1 account with 2 phones |
| 218-4 | Phone number that another account uses | Orange line on the card + the Profile note; name and photo on the web; an Audit line |
| 218-5 | Sajjad (server, no nearby link) sends text + a photo to an offline-only account | Clock, no red mark, no upload; when she joins (or comes nearby) it arrives by itself |
| 218-6 | Admin blocks an offline account in the app, then it joins the server | The web shows her Suspended/Blocked; she can't send |
| 218-7 | The recovery code written offline | Works after joining (Account → recovery) |
| 218-8 | Turn the Wi-Fi off during the join, then on | No `sumaira37`, no stuck state, one account |
| 218-9 | The phone was linked to another account on the server | U-3 device card; after the admin removes the phone → Try again works |

---

## 9. Files touched (nothing else)

**Backend (zip 1):**
- `internal/store/store.go` (v58);
- `internal/httpapi/api.go` (`writeErrorCode`), `auth.go`, `admin_blocks.go`, `contacts.go`, `profile.go`, `admin_panel.go`, `admin_216.go`;
- new `internal/httpapi/users_resolve_218.go`;
- `cmd/server/main.go` (1 route);
- `internal/httpapi/adminweb/views.js` + `styles.css` (tag);
- new `internal/httpapi/offline_join_218_test.go`.

**App (zip 2):**
- `lib/features/authentication/data/services/offline_account_sync_service.dart`;
- `lib/core/services/locallink_api.dart`;
- `lib/core/services/local_store.dart` (new methods only);
- `lib/features/account/data/repositories/account_repository.dart`;
- `lib/features/directory/data/repositories/directory_repository.dart`;
- `lib/features/directory/data/peer_photo_book.dart` (1 method);
- `lib/features/messaging/data/services/reliable_messaging_service.dart`;
- new `lib/features/messaging/data/services/not_on_server_218.dart`;
- `lib/features/messaging/presentation/widgets/message_options.dart`;
- `lib/features/messaging/presentation/screens/chat_screen.dart`;
- new `lib/features/home/presentation/widgets/join_cards218.dart`;
- `lib/features/home/presentation/screens/home_screen.dart`;
- `lib/features/account/presentation/screens/profile_screen.dart`;
- `lib/features/admin/presentation/screens/admin_users_screen.dart`;
- `lib/app/app_dependencies.dart`;
- `pubspec.yaml` + `lib/core/config/app_build.dart` (version);
- new tests `bundle218b / 218b_api / 218c / 218d / 218e`;
- old test `nearby_190_test.dart:291` (version line only);
- `TODO_218.md`, `APPLY_218.md`.

## 10. Build order
1. 218a backend + `go test ./...` green → backend zip + `APPLY_218.md` (backend part).
2. 218b (join) → its tests + 216f + 214g.
3. 218c (id move) → its tests + 195b / 195c / 177 / 202 / 204.
4. 218d (not on server) → its tests + 203 / 205 / 197 / 162 message tests.
5. 218e (cards / notes) → its tests + 183 / 214e / 216 UI tests; `ui_check_218.jpg`.
6. Version, full test run in chunks (never in parallel), analyze ≤ baseline, app zip with one commit.
