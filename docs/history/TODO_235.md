# TODO 235 - one phone, one account: a second account must prove the old one, or it is blocked

Status: BUILT (2026-11-09) in app 3.0.72+143 + backend zip 230-231-235. See `APPLY_235.md`.
Base: app `e5f7494` (3.0.71+142) · backend `1f995eb` + 230 + 231 (zip `locallink_backend_230-231_2026-11-09.zip`, not pushed).
New version: **3.0.72+143** (234 was undone, so 235 takes its number). App DB stays **34**. Backend: maybe 1 new table (see B).
Delivery: ONE app zip (one commit) + ONE backend zip (230 + 231 + 235). **Deploy the backend first.**

---

## 0. Your words and choices (2026-11-09)
- "Undo last changes. If two accounts are found, the user confirms their account with the old password or the recovery key, else block the account instantly. Even when the user is offline."
- "Only web admin can merge accounts in admin panel." -> covered by the admin "New owner" button (C3). No other merge.
- Checkboxes:
  - Undo: **all of 234** (done: the 234 zips and `APPLY_234.md` are deleted, `TODO_234.md` is marked UNDONE, the backend zip is back to 230-231; the 234 code is kept only as `undone_234_app.patch` / `undone_234_backend.patch` for reference).
  - Which account is blocked: **the NEW one**. The old account is never touched.
  - Offline check: **recovery code works offline**; the **old password needs the server**.
  - Phone given or sold to another person: **only the web admin unblocks it** ("New owner").

### What 234 did that is now gone (so you know)
- A new account on a known phone became a new contact with the old chat read only -> **gone**. Now the new account is blocked until it proves the old one.
- The server moved a reinstalled phone to the new account -> **gone**. Now the server refuses it (C1).
- The no-password upload of an offline account (B1/B2), no pairing code (B4), "Waiting for the server" card (B5), the Windows test fix (C) -> **also gone** (you chose undo all). Tell me if you want any of them back in 235.

---

## 1. The rule in plain words
**One phone = one account.** "Same phone" = the same phone id (`android-…`, it stays the same after a reinstall) or the same hardware id.

Example: Ali's Note had **@ali**. The Note is reinstalled and someone makes **@ali2** on it.
- @ali2 is **blocked** as soon as it meets any phone that knew @ali, or the server.
- To use the Note again the person must prove "I am @ali":
  - **Recovery code + a new password**: works with no server (friends' phones check it).
  - **Old password of @ali**: only when the server is reachable.
- Proved -> the Note **becomes @ali again**. Old chats with friends go on, calls work. @ali2 is dropped (it never had contacts).
- Can't prove -> @ali2 stays blocked. Only the web admin can free the phone ("New owner").

Not a duplicate (no block):
- The same account signs out and in again on its phone.
- 218: an offline account (`offline_…`) that got its server id (same keys).
- A phone nobody knew before.

---

## 2. How each side finds out (offline too)

### A. On a friend's phone (offline, WD / LAN / mesh / QR) - the main case
File: `pairing_service.dart`, `directory_repository.dart`, `sealing_peer_transport.dart`, new `duplicate_guard_235.dart`.
- When a card / add request / friend link / hello comes from a phone id this phone knows, but the account is different and the key is different (the same checks as 234 A2, without the 218 moves) -> **duplicate**.
- The friend's phone then:
  1. does NOT add @ali2, drops its messages and calls (log `DUP_ACCOUNT_BLOCKED`);
  2. keeps @ali and the chat exactly as they are (not read only);
  3. sends one "proof needed" note back to the Note: @ali's own signed card (name, photo, id). The Note needs it to show "This phone is @ali's" and to know which account to prove;
  4. the 10-minute "already handled" mute (L1 in the logs) is keyed by phone + account, so the note is not swallowed.

### B. On the server (online)
File: `auth.go` (register + login), new `duplicate_235.go`.
- A NEW account on a phone id / hardware id that already belongs to another active account -> **409 `DUPLICATE_ACCOUNT`** with the old @name (only when the hardware id matches, like 218) instead of today's `DEVICE_TAKEN` card.
  The new account is NOT created. The server keeps a list "waiting for proof" for the admin page (new table `duplicate_waits`: phone id, old user, new name, first seen, last seen).
- An offline account that tries to upload from such a phone gets the same answer.

### C. On the phone with the new account (the Note)
File: new `lib/features/authentication/presentation/screens/confirm_account_235.dart`, `home_screen.dart`, `offline_account_sync_service.dart`, `local_store.dart`.
- On the "proof needed" note (A3) or `DUPLICATE_ACCOUNT` (B) the app **blocks itself at once**:
  - a full screen "This phone already has an account" (mock) over everything; no chats, no calls, no adding;
  - saved in settings, so it stays blocked after a restart and offline.
- The note is checked first: it must be a correctly signed card for **this** phone id. (Risk: someone could make a fake card with this phone id - see Risks.)

---

## 3. Proving "I am @ali"

### P1. Recovery code (offline, the main way)
- New: every account has a **recovery key** made from its recovery code (PBKDF2 180000 + the user id -> an Ed25519 key). Only the **public** half goes into the person's signed card (`recovery_key`). The code itself never leaves the phone.
  - Safe to share: the code has about 100 bits, it can't be guessed from the public key.
  - Made when the account is made, when the code changes (222 reset / rotate), and once at the first start of 3.0.72 when the phone has the code saved.
- The Note: the person types the recovery code + a new password. The phone makes the key and signs "@ali is back on phone X with these new keys" (a **return card**).
- Friends' phones check the return card with @ali's `recovery_key` -> they take the new keys for @ali, unblock, the old chat goes on. The return card is passed on to other phones that knew @ali (they check it the same way).
- When the server is reachable later: the Note runs the 222 reset with the code + the new password lock (already supported by the server: `new_password_hash`). Then it is a normal server account again.
- Limit: a friend whose copy of @ali's card is from before 3.0.72 has no `recovery_key` -> can't check it offline -> the Note shows "Connect to the server once to confirm" for that friend. (After 3.0.72 is out a few days, almost every card has it.)

### P2. Old password (needs the server)
- The Note signs in to @ali with the old password (normal login; the phone id already belongs to @ali, so it works today).
- The server then tells friends the new keys (check in the build how a reinstalled phone's new keys reach friends today; if there is no safe way, add a server-signed key note that travels like the 212 block cards).

### P3. Recovery code online
- Same as the 222 "Forgot password": code + new password at once.

---

## 4. Web admin
File: `admin_panel.go`, `adminweb/views.js`, `adminweb/app.js`.
- Users page: a new card **"Waiting for proof"** (from `duplicate_waits`): "@ali2 tried to join on @ali's phone (Note 60x) · 9:40".
  - **New owner** button (Owner admins with Manage users, like Block): asks "Give Ali's phone to a new person? @ali keeps the account and the chats but loses this phone." -> type the name to confirm. Audit line `duplicate_new_owner`.
  - Then: the phone leaves @ali, the next upload of @ali2 works, and a server-signed "new owner" card goes to all phones (the 212 block channel) -> friends' phones stop blocking @ali2 and may add it as a new contact.
  - **Dismiss** removes the line (nothing changes, the block stays).
- No other merge anywhere.

---

## 5. Texts (mock)
- Block screen title: "This phone already has an account"
- Line: "Ali (@ali) used LinkUp on this phone. Confirm it's you to keep using it."
- Tabs / buttons: "Use recovery code" (code field + "New password" + "Confirm password"), "Use old password" (needs the server: "Connect to the server to use the old password.")
- Small grey line: "New owner of this phone? Ask the admin."
- Wrong code: "That code is not right."
- Friend's phone, none on screen. Log only.
- Admin card title: "Waiting for proof"

---

## 6. Files (expected)
App:
- new `lib/core/identity/recovery_key_235.dart` (make the key, sign / check a return card)
- new `lib/features/directory/data/services/duplicate_guard_235.dart` (detect, block, proof note)
- new `lib/features/authentication/presentation/screens/confirm_account_235.dart`
- `pairing_service.dart`, `directory_repository.dart` (card `recovery_key`, return card, mute key), `directory_models.dart` (`recoveryKey` field)
- `sealing_peer_transport.dart` (open the proof note / return card from a new key once)
- `local_store.dart` (block flag, become @ali again), `offline_account_sync_service.dart` (`DUPLICATE_ACCOUNT`), `locallink_api.dart`
- `home_screen.dart` / app start (show the block screen), `app_build.dart` + version pins
Backend:
- `auth.go`, new `duplicate_235.go`, `admin_panel.go`, `adminweb/*`, store migration (`duplicate_waits`), tests

## 7. Old tests that may change (will tell you)
- `bundle218*` tests that expect `DEVICE_TAKEN` for a new account on a taken phone -> `DUPLICATE_ACCOUNT` (only when the hardware id matches).
- Version pins.

## 8. New tests (planned)
1. Friend's phone: known phone + new account + new key -> blocked, proof note sent, @ali unchanged.
2. Same account signing in again / 218 offline id move -> not blocked.
3. The 10-minute mute does not swallow a different account.
4. The Note: proof note for another phone id -> ignored; for this phone id -> block screen, stays after restart.
5. Recovery key: same code + user id -> same key; another code -> another key.
6. Return card: right code -> friends take the new keys and unblock; wrong code -> refused; card with no `recovery_key` -> "connect to the server".
7. Return card passed on to a third phone that knew @ali -> accepted there too.
8. Server: new account on a taken phone (same hardware) -> 409 `DUPLICATE_ACCOUNT` + `duplicate_waits` row; other hardware -> today's answer.
9. Server: old password login on the phone -> works; then not blocked.
10. Admin New owner -> phone leaves @ali, @ali2 can join, signed card made, Audit line; Dismiss -> nothing changes.
11. Widget: block screen texts, both ways, wrong code text.

## 9. Phone tests (APPLY_235 will list them)
1. Note reinstalled, new account offline, meets the C71 on WD -> the Note shows the block screen; the C71 log `DUP_ACCOUNT_BLOCKED`.
2. On the Note: recovery code + new password -> the Note is @ali again; chat + call with the C71 work offline.
3. Same as 1 but with the server: "Use old password" -> @ali again.
4. Wrong code 3 times -> still blocked.
5. Web admin: "Waiting for proof" shows the line; New owner -> the new account joins.

## 10. Risks
- **Fake proof note:** a phone could make a card with someone else's phone id and lock that phone's NEW account. The old account is never hurt. Fix: the web admin "New owner", or the person proves the old account.
- **No recovery code saved on the old phone** (very old accounts) -> no `recovery_key` -> offline proof is not possible; the old password with the server or the admin.
- **Forgot both** -> only the admin (New owner), and the old chats stay with @ali.
- Phones that never knew @ali accept @ali2 until the server or the Note's own block stops it (the Note blocks itself as soon as it meets one friend or the server).
- Bigger than 234 (new key, new card field, new screen, backend list). About the same risk as 222.

## 11. Order
1. Mock (block screen + admin card) -> you check it.
2. Backend (B, C, admin) + tests.
3. App (A, C, P1-P3) + tests + full suite.
4. `APPLY_235.md`, one app zip, the backend zip (230 + 231 + 235), `PENDING.md`.

---

## 235 ADDITION (2026-11-09) - "Save on WhatsApp" instead of "I saved it"
Status: BUILT (2026-11-09) into the 235 app zip (still 3.0.72+143, not pushed). Mock v3 approved. See `APPLY_235.md` (Save on WhatsApp part, phone tests 6-9). Backend: no change.

Your choices (checkboxes):
- Replace "I saved it" with **"Save on WhatsApp"**. Copy stays. The screen closes when you come back from WhatsApp.
- No WhatsApp on the phone: **copy the code** + "Copied. WhatsApp is not on this phone." (the screen stays).
- **Everywhere a new recovery code is shown.**
- **Inside 235 now.**

Where (checked in the code - sign-up shows no code since bundle 110):
1. `confirm_account_235_screen.dart` done view: Copy + **Save on WhatsApp** (was "I saved it").
2. `recovery/presentation/screens/account_recovery_screen.dart` (222 Forgot password done): Copy + Share stay; big button **Save on WhatsApp** (was "I saved it"). With no code (offline reset) the button stays "OK".
3. `home/presentation/widgets/join_cards218.dart` card "Your recovery code changed": Copy + **Save on WhatsApp** (was "I saved it").
4. `account/presentation/screens/profile_screen.dart` "New recovery code" box: Copy + **Save on WhatsApp** (was "Done").
Design (mock v2, 2026-11-09 - v1 was "ugly", replaced):
- The code sits in ONE soft blue box ("Recovery code" label); **Copy** (and Share in 222) are small icon+text buttons inside the box, top right.
- Under the box: lock line "Keep it private. It lets you set a new password." + "Your old code no longer works."
- Full screens (235, 222): green tick circle on top, "Save your new recovery code now.", the green **Save on WhatsApp** button pinned at the bottom (round chat-bubble phone glyph, no WhatsApp logo), under it 'Opens WhatsApp · choose "Message yourself"'. The 222 "OK" circle becomes the same tick.
- Home card: code box + full-width green button (Copy inside the box).
- **v3 (user, 2026-11-09): the code is HIDDEN by default** (4 groups of 5 dots). Inside the box: **Show** (eye) / **Hide** (eye off), then Copy (+ Share in 222). Hidden again every time the screen / card / box opens. Copy and Save on WhatsApp work while hidden (they use the real code). Show is only on this phone; nothing is sent. Applies to all 4 places.
- Profile box: key icon, title, code box, green button, hint, "Close" (was "Done").

How it works:
- New small helper `lib/core/services/whatsapp_share_235.dart` -> native call (MainActivity method channel): `ACTION_SEND` text, package `com.whatsapp`, then `com.whatsapp.w4b` (Business). Not found -> returns false. No new permission, no manifest change (the manifest count pin stays 25).
- Message: `My LinkUp recovery code (@ali): K7QM2-XPA9T-RC4WN-E8D3F - keep it private.`
- WhatsApp opened -> when LinkUp comes back to the front, the screen / card / box closes (same as "I saved it" / "Done" before).
- Not opened -> code copied + snackbar, nothing closes.

Risk (tell the user): if you go back from WhatsApp without sending, the screen still closes and the code is not shown again. Copy is there before you tap. A new code can always be made in Profile > Recovery code.

Tests: the 4 places show "Save on WhatsApp" (no "I saved it"); the code is hidden at open, Show shows it, Hide hides it, Copy copies the real code while hidden; WhatsApp found -> closes after resume; not found -> copied + snackbar + stays; the message text; Kotlin source check (ACTION_SEND, both package names, catch ActivityNotFoundException). Old tests that tap "I saved it" / `join218_saved` / `dup235-saved` / "Done": only the tap target changes (I will tell you).
Files: the 4 screens above, new `whatsapp_share_235.dart`, `android/app/src/main/kotlin/.../MainActivity.kt`, `test/bundle235_test.dart` (+ any old test that taps the old button).
