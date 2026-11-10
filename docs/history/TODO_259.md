# TODO 259: no "Unknown user" after a reinstall + login (every phone)

Status: WRITTEN, not built (freeze on; build only when the user says "Build").
Base: app `39f16da` (3.0.76+147, pushed), backend `2b3d9a0`.
Plan: backend zip first (one new route), then ONE app zip, ONE commit, version 3.0.77+148.
User answers (11 Nov): write TODO now (a + b + c); backend change OK; "Unknown user" was seen on an OLDER version (not 3.0.76).

## What the user said
"Every phone shows Unknown user if reinstalled login user."
Screenshot `uploads/Screenshot_20261110-140823.png`: Chats -> one row "Unknown user - You: hello".

## Step 0: test 3.0.76 first (no code)
3.0.76 already fixed part of this (TODO 258 B/C):
- the text is "LinkUp user", never "Unknown user";
- `NameBook258` keeps the last name seen (it is lost on an uninstall, because it is app data);
- the chat list re-reads names when a card arrives (`NameBook258.changes`);
- a failed contacts pull is tried again every 30 s, up to 10 times (`CONTACTS_SYNC_RETRY`).

Test: on ONE phone, uninstall, install 3.0.76, log in with the server ON, wait 1 min, send the log.
Look for `CONTACT_PUSH`, `CONTACTS_SYNC_FAILED`, `CONTACTS_SYNC_RETRY`, `NAME_MISSING`.
If the names come back, parts A–C still matter for: the server OFF, a friend's old phone, and people who are not contacts.

## Why it happens (checked in code)
A chat row is keyed by the friend's PHONE id. Its name comes from
`LocalStore.peerDisplayName(id)` (`lib/core/services/local_store.dart` ~1904):
1. `directoryProfile(id)` or `directoryProfileByDeviceId(id)` (a saved card);
2. else the contact row with that phone (`contactUserOfDevice258`);
3. else the custom name;
4. else `NameBook258` (the last name seen);
5. else "LinkUp user".

After an uninstall, the app data is empty. The phone id stays the same (`android-` + hardware fingerprint, `local_store.dart:249`), but the keys are new. The chats come back from the server message sync, but the names only come back if a card is saved again:

- **R1 (server ON):** `syncServerContacts()` (`directory_repository.dart` ~1932) pulls the friend list, then calls `getProfile(userId)` for each friend (server `/directory/user/{id}`). The card it saves has the friend's CURRENT phone.
  - If the chat is with the friend's OLD phone id, there is no match. `personOfPastPhone237()` (the 237 list of past phones, filled by this pull) is NOT used in `peerDisplayName`, only in the chat merge.
  - There is no success log, so a log can't show that the pull worked. The C71 log at 16:35 has `CONTACT_PUSH ok n=2`, then only `CONTACTS_SYNC_FAILED`.
- **R2 (server ON, not a contact):** someone you chatted with but never added. The pull never brings that card. The server has no "who owns this phone" lookup (only `/directory/user/{id}`, `/phone`, `/name`).
- **R3 (server OFF, the deadlock):** a friend's phone still has my OLD card and seals to my old key.
  - My reinstalled phone has no card for that friend, so `SealingPeerTransport` (`sealing_peer_transport.dart` ~436) drops it with `SEAL_OPEN_FAILED reason=not_a_contact`. If I do have the card: `reason=unknown_recipient_key`.
  - Nothing is sent back. The friend never gets `sender_key_mismatch`, so the 258 card ask (`askOwnCard258`) never starts.
  - Neither side learns the other's new card, so no name and no messages until the server is reached.

---

## A. App: use what this phone already knows (R1)

### A1 Past phones in the name lookup
- `local_store.dart` `peerDisplayName()`: after the `contactUserOfDevice258` step, add:
  `if (profile == null) { final owner = await personOfPastPhone237(id); if (owner != null) profile = await directoryProfile(owner); }` (in try).
- Keep the lines between `contactUserOfDevice258(id)` and `_customNameFor` as they are. Grep `test/` for exact-string checks before editing.
- Pass `ids: [id, owner]` to `NameBook258.pick` so the name is remembered for both.

### A2 Logs that show the pull worked
- In `syncServerContacts()`, after `saveServerConfirmedContactIds`, add a new log:
  `CONTACTS_SYNC_OK|n=<remote count> new=<added here> past=<friends with past phones>`.
- After the loop, notify the chat list once: `NameBook258.instance.notifyChanged()`. The `getProfile` calls are unawaited, so also notify when each one ends (`.then`).

### A3 First pull right after login
- The login hook (`app_dependencies.dart` ~1652) already calls `syncServerContacts()`.
- Change: right after a successful sign-in, skip the `ServerReach.shouldSkip('contacts_sync')` check once (new `force: true` argument). A failed call just before the login must not delay the first pull.
- Log: `CONTACTS_SYNC_FORCED|why=sign_in`.

## B. Server lookup "who owns this phone" (R2) — backend + app

### B1 Backend: new route `GET /api/v1/directory/device/{id}`
- File: NEW `internal/httpapi/directory_device_259.go`. Route in `cmd/server/main.go` next to the other `/directory/` routes.
- Auth: `currentAccountForDevice(r)`. If it fails, return 401.
- Owner: `SELECT user_id FROM devices WHERE id=?` (any status). A phone given to a new owner (235) already has the new owner's `user_id`. If there is none, return 404.
- Allowed only when ONE of these is true. Otherwise return the SAME 404 `user not found`, so a stranger learns nothing:
  1. the requester and the owner are contacts, in either direction (`account_contacts`); or
  2. a message exists between ANY phone of the requester and this phone id:
     `SELECT 1 FROM messages WHERE (sender_id IN (<requester phones>) AND recipient_id=?) OR (sender_id=? AND recipient_id IN (<requester phones>)) LIMIT 1`.
- Answer: `a.directoryProfile(owner, requesterID)`, the same card as `/directory/user/{id}`. The number stays hidden unless public. `directory_sync_enabled=0` means 404 (the same rule as today). Add one field: `asked_device_id`.
- Close rows before nested queries (`SetMaxOpenConns(1)`).
- NEW test `directory_device_259_test.go`:
  - contact -> 200 with name;
  - message partner, not a contact -> 200;
  - stranger -> 404, body identical to an unknown phone;
  - the owner's old phone id -> 200 with the current card;
  - a phone moved to a new owner -> the new owner's card;
  - number hidden; sync off -> 404; no token -> 401.

### B2 App: fill the missing names
- `lib/core/services/locallink_api.dart`: add
  `directoryByDevice(String deviceId) => _directorySingle('/api/v1/directory/device/${Uri.encodeComponent(deviceId)}')`.
  A 404 from an older server -> null.
- `directory_repository.dart`: NEW `fillMissingNames259()`. It is called at the end of a successful `syncServerContacts()` and on app start once the server is reached.
  - For each id in `store.chatPeerIds()` (max 50 per run) where `peerDisplayName(id)` gives no card name and no custom name:
    - skip if it was asked in the last 6 h (in memory; a "none" answer is kept 6 h);
    - call `api.directoryByDevice(id)` (3 s timeout; stop the run at the first network error);
    - got a card -> `_saveServerProfile(card)`, `NameBook258.remember(name, [id, card.userId])`; if `card.deviceId != id`, `rememberServerPastPhones237(card.userId, [id], card.deviceId)`;
    - at the end -> `NameBook258.notifyChanged()`.
  - Log: `NAME_FILL_259|asked=<n> found=<n> none=<n>`.
  - The repo contract has many fakes, so call it only on the real class (`if (dir is DirectoryRepository)`). Store calls go in try.

## C. Offline: break the reinstall deadlock (R3) — app only

### C1 Tell the sender my new card
- `sealing_peer_transport.dart`: new hook `static void Function(String sender, String why)? onCantOpen259;`. Call it when a sealed DIRECT packet addressed to me is dropped with:
  - `not_a_contact` (the `theirs == null` branch), or
  - `unknown_recipient_key` after the reload retry still fails.
- Not for: group packets, block acks, blocked senders, `unknown_session_key` (166 already handles it), or a packet whose recipient is not my phone id.
- `directory_repository.dart`: NEW `cantOpenFrom259(sender, why)`, wired in `app_dependencies` like `onSenderKeyMismatch258`:
  - skip if the sender is blocked (phone or user);
  - at most once per phone per 10 min, and at most 20 phones per hour in total;
  - send MY public card in the open: the same payload as `_answerOwnCardAsk258` (`DIRECTORY_OWN_CARD_258`, no number unless public). Split the card building out of `_answerOwnCardAsk258` into one helper so both use it. The friend's 258 code takes it by itself (owner rule: `sender == card.deviceId`), forgets my old key, and resends;
  - then `askOwnCard258(sender, why: 'reinstall')`. The friend answers only if I am still his contact (the 258 rule, unchanged). My phone saves that card through `_saveVerified` (signature checked), so the name shows;
  - log: `KEY_REINSTALL_HELLO_259|to=<6> why=not_a_contact|unknown_recipient_key`.
- Risk (tell the user): anyone who sends a sealed packet to my phone gets my public card (name, username, public keys; no number). That person already had my phone id and old card. It is never sent to a blocked phone.

### C2 After the friend's card arrives
- `_handleOwnCard258`, when it is saved and the sender is NOT my contact: no auto re-add. Offline, the friend list comes back with the server (R1). The chat shows the name.
- Open question Q1 below.

### C3 NOT VERIFIED (check in the build, write it down)
- Do messages that the friend sent to my OLD key get sent again after he takes my new card? Expected: there is no ack, so they stay in his outbox, and `LinkKeyRefresh257` + forgetting the seal keys reseals them. If not, add a resend of unacked messages to that phone after `KEY_AUTO_258 why=owner`.

---

## D. Tests (app)
- NEW `test/bundle259_test.dart`:
  - A1: a chat with a friend's old phone id (in `PersonPhones237`) -> his name;
  - A2: the log line `CONTACTS_SYNC_OK|n=` after a good pull; `notifyChanged` fires;
  - A3: `force: true` runs even when `ServerReach` says skip;
  - B2: a chat peer with no name -> `directoryByDevice` is called once; a card is saved; the name shows; a second run within 6 h does not call again; 404 -> "LinkUp user" stays, no crash;
  - C1: `not_a_contact` -> one `DIRECTORY_OWN_CARD_258` + one `DIRECTORY_CARD_ASK_258` to that phone; a second packet within 10 min -> nothing; blocked phone -> nothing; group packet -> nothing.
- OLD TESTS: version lines only (`bundle223_logs`, `nearby_190`). Any other change gets listed in APPLY.
- Full Flutter suite + `flutter analyze` (no new warnings). Backend: `go test ./...` (`-p 1`). Kotlin: not touched.

## E. Install order
1. Backend zip: deploy and restart (the new route only; the database is unchanged).
2. App 3.0.77 on all 3 phones (install over the old app).
3. Reinstall test on one phone, server ON: names within 1 min (`CONTACTS_SYNC_OK`, `NAME_FILL_259`).
4. Reinstall test with the server OFF, near a friend on Wi-Fi Direct: `KEY_REINSTALL_HELLO_259` on the reinstalled phone, `KEY_AUTO_258 why=owner` on the friend, then the friend's name shows.

## Open questions
- Q1: offline, after a reinstall, should a friend who still has me as a contact come back into my People list by himself? My advice: no, only the name. The list comes back from the server.
- Q2: should people who turned directory sync off still show their name to someone they chatted with? My advice: no (the same rule as today).
