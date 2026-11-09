# TODO 237 - a friend on a new phone keeps ONE chat with all old messages

Status: PLAN only. Freeze ON. Nothing is built until you say "go ahead 237".
Base: app 235 (`3.0.72+143`) · backend 230 + 231 + 235. Version: the next free number (3.0.73+144 if 237 is built before 236, else 3.0.74+145).
App DB stays **34** (the new list is kept in the settings table as JSON). Backend: no new table, one new field.
Delivery: ONE app zip (one commit) + ONE backend zip. **Deploy the backend first** (the app also works on an old server, it just can't find old phones from before the update).

---

## 0. Your words and choices (2026-11-09)
- "When user is already added and any user add again just return all old chats if have, don't create new one."
- Checkboxes:
  - When it happens: **a friend reinstalled LinkUp / got a new phone** and was added again -> the old chat was gone, an empty new chat showed.
  - New phone id: **old + new messages in ONE chat**.
  - "Delete chat" / "Clear chat" done before: **stays deleted** (not brought back).
  - Re-adding today erases the name you gave them and the private mark: **keep them** (bug fix).

## 1. Why it happens (checked in the code)
- Chats are saved by **phone id**, not by person (`messages.sender_id / recipient_id` = phone ids).
- When Ali gets a new phone, his card now points to the new phone id. His old phone id is forgotten (`directory_profiles` and `contacts` keep only ONE phone per person).
- So the chat opens with the new phone id -> `messagesFor(newPhone)` -> empty. The old messages are still on your phone under the old phone id, but nothing links them to Ali any more.
- Removing a contact does **not** delete messages (checked: `removeContact` only deletes the contacts row). So nothing is lost; the messages just need to be linked again.
- Extra bug: `LocalStore.addContact` uses "insert or replace", so adding someone who is already a contact wipes `custom_name`, `is_private` and `added_at`.

## 2. Example
Ali (@ali) chatted with you on his Note. He buys a new phone, installs LinkUp and signs in as @ali. You add him again (or he re-appears in your list).
- Today: Chats shows Ali with an empty chat. The old messages are hidden.
- After 237: Chats shows **one** Ali row. Open it: all old messages on top, the new ones under them, in time order. New messages go to his new phone.

---

## A. Remember every phone a friend used (app)
New file `lib/core/services/person_phones_237.dart` (settings key `person_phones_237`, JSON: user id -> up to 10 old phone ids, with the time each one was replaced).
Learned ONLY from places that are already trusted:
1. `LocalStore.saveDirectoryProfile`: the saved card for the same person had another phone id -> keep the old one. (Cards only get here after the signature + same-key checks, or from the server.)
2. `LocalStore.addContact`: the contact row had another phone id -> keep the old one.
3. The server contacts list: new field `past_device_ids` (part D) -> fills in moves that happened BEFORE this update.
Safety rule: if an old phone id is now the CURRENT phone of a different person (235 admin "New owner"), it is taken out of the old person's list. A stranger can never pull your chats with Ali into his chat.

## B. One chat per person (app, read side only - no message is moved or rewritten)
- Open chat: `history()` and the cached history load messages of the current phone **plus** the person's old phones (`messagesForParticipants`-style query, both directions, only with me).
- Chats list: the old phone id now resolves to Ali (`userIdForDevice`), so the existing "one row per person" rule (`_collapseDeviceDuplicates`) shows one row. Preview = the newest message of all his phones; unread count = sum.
- Mark as read, Clear chat, Delete chat: apply to all his phones (Delete / Clear still = gone for good, the 197a "never bring back" list stays).
- Waiting messages (not delivered yet) to his old phone: sent again to his new phone once, then marked as before.
- Sending, calls, files: always to his current phone (no change).
- No new text or button on screen (so no mock). Tell me if you want a small line "Ali's new phone" between the old and new messages; that needs a mock first.

## C. Adding someone who is already a contact (app)
- `LocalStore.addContact`: update the phone id but **keep** the name you gave, the private mark and the first "added" time. A new name is used only when one is typed.
- Add on a person who is already a contact opens the same chat (no new row, no new chat).

## D. Server: old phones of my contacts (backend)
File: `internal/httpapi/contacts.go` (`accountContactsForUser`).
- Each contact gets `past_device_ids`: that person's other phone ids on the server (any status, newest first, max 10, not the current one).
- Only for people in MY contacts list (same as today's `device_id`). Old apps ignore the field.
- Test: `internal/httpapi/contacts_237_test.go`.

---

## Not covered (limits)
- **My own** new phone: messages saved only on my old phone are not on the new one (as today). Online history that the server still has follows the server rules as today.
- Messages deleted with "Delete chat" / "Clear chat" stay deleted (your choice).
- Offline only, never on the server, friend on a new phone: your phone can't prove the new phone is Ali (new keys), so the chats join only after one of you reaches the server once, or after you add him again while online.
- A phone the admin gave to a new owner (235): the old messages from that phone stay with whoever owns that phone id now (same as today).

## Files (expected)
App:
- new `lib/core/services/person_phones_237.dart`
- `lib/core/services/local_store.dart` (saveDirectoryProfile / addContact hooks, person-wide message query, previews, unread)
- `lib/core/models/account_restore.dart` (`AccountContact.pastDeviceIds`)
- `lib/features/directory/data/repositories/directory_repository.dart` (server list -> A3)
- `lib/features/messaging/data/services/reliable_messaging_service.dart` (`history`, waiting messages to the new phone)
- `lib/features/messaging/data/repositories/messaging_repository.dart` (`cachedHistory`)
- `lib/features/messaging/data/message_delete_actions.dart` (Clear / Delete for all his phones)
- `lib/features/home/data/repositories/home_repository.dart` (`userIdForDevice`), `lib/features/home/presentation/screens/home_screen.dart` (preview + unread per person)
- `app_build.dart`, `pubspec.yaml`, version pins; new `test/bundle237_test.dart`
Backend:
- `internal/httpapi/contacts.go`, new `internal/httpapi/contacts_237_test.go`

## Old tests that may change (I will tell you)
- None expected. If a test checks `addContact` replacing the name, only its expectation changes.

## New tests (planned)
1. Card with a new phone for Ali -> the old phone is remembered; a card for another person never adds phones to Ali.
2. Ali's chat shows old + new messages in time order; only messages with me.
3. Chats list: one Ali row; preview = newest; unread = sum.
4. Clear / Delete chat removes old + new; they never come back.
5. Server list with `past_device_ids` fills the list (move before the update).
6. A phone that now belongs to another person is taken out (235 New owner).
7. Re-add keeps the custom name, private mark and first added time.
8. Waiting message to the old phone goes to the new phone once.
9. Backend: `past_device_ids` only for my contacts, max 10, never the current phone.

## Phone tests (APPLY_237 will list them)
1. Chat with Ali (C71). Ali reinstalls with a new phone id or uses another phone, signs in as @ali, online.
2. Your Chats: one Ali row, last message shown. Open: old messages + new. Send -> reaches his new phone.
3. Remove Ali, add him again -> the same chat with all messages, your custom name still there.
4. Clear chat on Ali -> empty; reopen the app -> still empty.

## Order
1. You push 235 -> I check it.
2. You choose the order of 236 and 237 and say "go ahead".
3. Backend D + test -> backend zip. App A, B, C + tests + full suite -> `APPLY_237.md`, one app zip, `PENDING.md`.
