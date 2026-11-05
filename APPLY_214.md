# APPLY 214: web makes any user an admin, crown on admin photos

App 3.0.55+126 · DB v34 (no change) · no new permissions.
Backend: separate zip `locallink_backend_214_2026-11-05.zip` (migration v57 `app-user-admins`). **Deploy the backend first.**
**Push order: 211 → 212 → 213 → 214** (app + backend each time, backend first). 214 is built on top of 213.

## What changed
**A. Web panel (backend)**
- Users page: new **Admin** column and **Make admin / Change level / Remove admin** (Owner only). Pick the level per user: Owner (default) / Operator / Security / Auditor.
- The last Owner can't be removed. Disable user → admin removed too. Suspend keeps the level.
- New: `POST /api/v1/admin/user-admin`, `POST /api/v1/admin/device-session` (else `403 not_admin`), `GET /api/v1/admin-badges`.
- Each change makes a server-signed admin card `{kind:"admin_badge", user_id, role, on, seq, ...}`.
- A user made admin from the web can't sign in to the web panel with an app account (web-only admins keep their web login).

**B. The admin's phone**
- No admin sign in / sign out in the app any more. The phone asks the server by itself: 6 s after start, on every server connect, every 10 min, on a server push, and in the background.
- The admin session is saved on the phone, so the panel opens with no internet (offline tools: block, app version; Owner only).
- Notifications: "You are now an admin 👑" / "You are no longer an admin" / "Your admin level is now X". Nothing for users who were never admin.
- Sign out keeps admin and the crown. Reset clears them. Another account on the same phone → the old admin is cleared.

**C. Crown 👑 for everyone**
- Small gold crown on the photo, top centre: chats, chat top bar, profile, people, groups, map, my own photo. Photo size unchanged; lock and presence dot stay where they were.
- The cards spread phone to phone (local Wi-Fi / Wi-Fi Direct / mesh), so phones that were never online see it too. Newest card wins; `on:false` removes the crown everywhere. Up to 200 cards, 20 per frame. Fake cards (not signed by the server) are refused.
- A blocked-by-admin photo (grey) shows no crown.
- Group member profile sheet: line "👑 Admin · Owner".

**D. Settings**
- The Admin section is gone (no "ADMIN" label, no "Admin panel", no "Tools for testing" there).
- Tag "👑 Admin · Owner" under my name when I'm admin.
- Tap my photo: admin → Gmail-style sheet (me / Admin 👑 + level); not admin → Account opens.
- Sheet: the Admin row is off when my account is blocked by admin. Offline it says "Owner · Offline tools only · block, app version".

**E. Admin panel**
- Server says 401 → the phone asks for a new session by itself: new one → the panel reopens; not admin → closes with "You are no longer an admin"; no answer → closes with "Admin tools are not available right now" (kept).

**G. Offline re-login (same phone)**
- Account used online → Sign out → no server → same username + password (or recovery code) opens the same account. Only the last account is remembered.
- Logs: `AUTH_OFFLINE_LOGIN ok/bad`, `ADMIN_ROLE_KEPT_AFTER_SIGNIN`, `ADMIN_ROLE_OTHER_ACCOUNT_CLEARED`.

## Android
- `MeshBlockPolicy.kt`: `admin_badge_digest` / `admin_badge_records` are one-step frames (like 212/213). New `Admin214Test.kt`.

## Files deleted (the zip can't delete, run these)
```
git rm -q lib/features/admin/presentation/screens/admin_login_screen.dart
git rm -q lib/features/admin/domain/admin_login_error.dart
git rm -q test/bundle211e_login_error_test.dart
```

## Different from the TODO
- The old "pending revoke" stays: an old 211 web sign-in on the phone is ended on the server at the next connect.
- No crown in the group QR / group link member previews (small lists).
- "Admin · Owner" line only in the group member profile sheet, not in 1:1 chat info.
- Skipped A9 (Admin sessions page "App · phone"): not needed now that the app has no web login.

## Old tests (changed, as allowed)
- `bundle183`: order list without ADMIN / Admin panel; "ADMIN" and "Tools for testing" now expected absent in Settings.
- `bundle196` L551–552: no `MessengerGroupLabel('Admin')`.
- `bundle211b`: sign-out cases dropped (no admin sign out); login screen check → "file does not exist"; panel text → "Admin tools are not available right now".
- `bundle211c`: switch sheet rows rewritten (no Sign in / Sign out); flow test uses a real fake repository.
- `bundle211e`: deleted (its screen is gone).
- `bundle211f`: online + 401 → session kept, new message.
- `nearby_190`: version → 3.0.55+126 (as every bundle).

## Checks here
- `flutter test`: see the numbers in the reply. `flutter analyze`: see the reply.
- Go: `go test ./...` ok (7 new tests). Kotlin compile check.
- New tests: `bundle214a..g_test.dart` (+ `support/b214_fakes.dart`).

## Phone tests
- [ ] 214-1 Web (Owner): Users → Sajjad → Make admin (Owner). Realme online gets the notification within seconds; Settings has no Admin section; tap photo → me / Admin 👑 → panel opens with no password.
- [ ] 214-2 Realme offline (airplane + Wi-Fi Direct to ZTE): panel still opens; 212 block and 213 minimum version still work offline.
- [ ] 214-3 ZTE sees the 👑 on Sajjad's photo (chats, chat top bar, profile, people, groups, map), first from the server, then also over Wi-Fi Direct / mesh with ZTE never online.
- [ ] 214-4 Web makes Sumaira (ZTE) an admin while ZTE is offline: Realme passes the card on, so the crown shows on both; ZTE's admin tools turn on when it reaches the server.
- [ ] 214-5 Web level → Operator: block / minimum version say "Only the owner admin…"; crown stays.
- [ ] 214-6 Web Remove admin: notification "You are no longer an admin", the panel closes, the crown goes on all phones. Old blocks stay (an Owner can undo them).
- [ ] 214-7 App closed on Realme during 214-1 / 214-6: the change arrives in the background (notification).
- [ ] 214-8 A non-admin taps the Settings photo → Account opens, no switch.
- [ ] 214-9 Logs: `ADMIN_ROLE_*`, `ADMIN_BADGE_*`.
- [ ] 214-10 ZTE: sign in online → send a few messages → Sign out → Wi-Fi and data off → same username + password → same account with old chats. Also the recovery code in the password box, and a wrong password ("That username and password do not match.").
- [ ] 214-11 Realme (admin) → Sign out → airplane → sign in again → crown on my photo and Admin 👑 in the sheet at once; the panel opens with the offline tools.
- [ ] 214-12 Realme Sign out → sign in online as a normal test user → no crown, no Admin row.

## PowerShell (backend, first)
```
cd <LocalLinkBackend folder>
git pull
Expand-Archive -Force "$HOME\Downloads\locallink_backend_214_2026-11-05.zip" .
git add -A
git add -f APPLY_214.md
git commit -m "v3 bundle 214 backend: app users as admins (make/remove admin with role, device admin session, signed admin cards, web Users page)"
if ($LASTEXITCODE -eq 0) { git push }
```
Then deploy the server as usual (migration v57 runs on start).

## PowerShell (app)
```
cd "C:\Users\sajjad Ahmed\Downloads\LocalLink"
git pull
Expand-Archive -Force "$HOME\Downloads\locallink_app_214_2026-11-05.zip" .
git rm -q lib/features/admin/presentation/screens/admin_login_screen.dart
git rm -q lib/features/admin/domain/admin_login_error.dart
git rm -q test/bundle211e_login_error_test.dart
git add -A
git add -f APPLY_214.md TODO_214.md
git reset -q -- android/local.properties
git commit -m "v3 3.0.55+126 bundle 214: web makes any user an admin (no admin login or logout in the app), admin saved for offline and refreshed in the background, crown on admin photos for everyone (also over mesh), Gmail-style switch only, Settings Admin section removed, same account and admin after Sign out and offline sign in"
if ($LASTEXITCODE -eq 0) { git push }
flutter clean; flutter pub get; flutter test; flutter build apk --release
```
