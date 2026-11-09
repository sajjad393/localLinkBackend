# TODO 236 - the offline account goes up to the server by itself (no password, no code)

Status: PLAN only. Freeze ON. Nothing is built until you say "go ahead 236".
Base: app 235 (`3.0.72+143`, zip `locallink_app_235_2026-11-09.zip`) · backend 230 + 231 + 235.
Build this only AFTER you push 235 (I check your push first, then 236 goes on top).
New version: **3.0.73+144**. App DB stays **34**. Backend: no new table.
Delivery: ONE app zip (one commit) + ONE backend zip (236 only, on top of your pushed 235). **Deploy the backend first.**

---

## 0. Your words and choices
- 2026-11-09: "Don't ask for password or any code when offline account detect auto update to the server."
- Checkbox (2026-11-09): bring it back as **a new bundle 236** (235 stays as it is).
- Taken from the undone 234: B1, B2, B4, B5 and the Windows test fix (C).
- **NOT taken:** 234 B3 ("the server moves a reinstalled phone to the new account"). It goes against 235 (one phone = one account). 235 wins: a new account on a known phone still waits for proof.

## 1. In plain words
Example: Sara made **@sara** with no server (offline account). Later the phone reaches the server.
- Today: if the phone lost the saved password, Home opens "Publish your account - confirm your password" (password, maybe a pairing code).
- After 236: nothing is asked. The phone sends @sara up by itself. Sara only sees the normal card "Your account is on the server now".
- The plain password is never sent in this mode. Only the saved password lock (salt + hash) goes, the same way the 235 / 222 offline reset already does it.

---

## B1. No "Publish your account" sheet (app)
File: `lib/features/home/presentation/screens/home_screen.dart`
- Remove the auto-open on Home (`_maybeOfferPublish`) and the sheet (`_showPublishSheet`: password, username, pairing fields, rename step).
- The banner "Publish" button / banner tap runs `OfflineAccountSyncService.autoPublish(from: 'banner')` (silent, same lock as today). The button shows a small spinner while it runs; then the normal 218 cards show the result.
- Name taken: the silent 218 rule stays (auto rename + friends are told). Nothing is asked.

## B2. No saved password -> upload with the saved password lock (app + backend)
- Today: `autoPublish` stops with `OFFLINE_ACCOUNT_FAIL code=no_secret reason=needs_password`.
- App (`offline_account_sync_service.dart`, `locallink_api.dart`): with no saved password, send the saved **salt + hash + iterations** of the offline account (made by `saveOfflineAccount`) with `offline_user_id` and the recovery code.
- Backend (`internal/httpapi/auth.go`, register):
  - new optional fields `password_hash`, `password_salt`, `password_iterations`;
  - accepted ONLY with a valid `offline_user_id` (`offline_...`) and an empty `password`;
  - size checks; iterations 100000-2000000; the 230 re-hash moves it to 600000 at the next login.
- Name taken in this mode: no "same name + same password" try (no plain password) -> straight to the silent rename.
- **235 stays:** a new account on a known phone still gets `409 DEVICE_TAKEN + duplicate:true` -> "Confirm your account".
- Logs: app `OFFLINE_ACCOUNT_PUBLISH mode=lock`; backend `auth register mode=lock`.

## B4. Accounts made offline skip the pairing code (backend + small app part)
Files: `auth.go` (register), `join_relay_222.go` (carried join box), app join card.
- If `LOCALLINK_PAIRING_CODE` is set: a sign-up with a valid `offline_user_id` does not need it. Login and normal online sign-ups still need it.
- The app no longer shows "The server needs a pairing code" for an offline account.
- **Risk:** the phone itself says "made offline", so anyone could claim it. With a pairing code on the server, the code no longer stops new sign-ups. Without a code: nothing changes.

## B5. Old server (backend not updated yet)
- If the server still answers `PAIRING_REQUIRED` or does not know the lock fields, the app waits quietly and tries again later (no sheet, no code field).
- One info line on the card: "Waiting for the server to be updated." That's why the backend goes first.

## C. Windows test fix (test only)
- `test/bundle223_first_start_test.dart:114`: split the path on both `/` and `\`. No app change (an old test; only the path split changes).

---

## Files (expected)
App:
- `lib/features/home/presentation/screens/home_screen.dart` (B1)
- `lib/features/home/presentation/widgets/join_cards218.dart` (B4 / B5 card text)
- `lib/features/authentication/data/services/offline_account_sync_service.dart` (B1, B2)
- `lib/core/services/locallink_api.dart` (B2 fields)
- `lib/core/config/app_build.dart`, `pubspec.yaml`, version pins (4 in `bundle223_logs_test.dart`, 1 in `nearby_190_test.dart`)
- new `test/bundle236_test.dart`
Backend:
- `internal/httpapi/auth.go`, `internal/httpapi/join_relay_222.go`, new `internal/httpapi/offline_lock_236_test.go`

## Old tests that may change (I will tell you)
- Tests that open the publish sheet on Home (they would change to "no sheet opens"). Only an expectation changes.
- `bundle223_first_start_test.dart` (C).

## New tests (planned)
1. Home: no publish sheet ever opens; banner Publish -> `autoPublish(from: 'banner')`.
2. No saved password -> register sends salt + hash + iterations, never a password.
3. Backend: lock fields refused without `offline_user_id`, refused with a password, refused with bad sizes / iterations; accepted -> login with the real password works; re-hash to 600000.
4. Backend: pairing code skipped only for `offline_user_id` sign-ups; login still needs it.
5. Known phone + lock mode -> still `DEVICE_TAKEN + duplicate:true` (235 kept).
6. Old server answers `PAIRING_REQUIRED` -> no code field, quiet retry, info line.

## Phone tests (APPLY_236 will list them)
1. Make an offline account with no server, clear the saved password (or reinstall the build that loses it), connect to the server -> it goes up by itself; card "Your account is on the server now".
2. Sign in on another phone with the same password -> works.
3. Server with a pairing code -> the offline account still goes up; a normal online sign-up still asks for the code.
4. A known phone (235) -> still "Confirm your account".

## Risks
- B4 weakens the pairing code for new sign-ups (see B4).
- B2 trusts the hash the phone sends (the server can't check the password strength). The 230 re-hash fixes the rounds at the next login.

## Order
1. You push 235 -> I clone and check it.
2. You say "go ahead 236".
3. Backend B2 + B4 + tests -> backend zip.
4. App B1, B2, B4, B5, C + tests + full suite -> `APPLY_236.md`, one app zip, `PENDING.md` update.
