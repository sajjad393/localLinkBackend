# TODO 221 — Chats from phones + server postman + soft delete

Status: PLAN (complete, code-checked 7 Nov 2026). Freeze ON. Build only after "go ahead 221", and **after 220 is pushed and phone-tested** (221 uses the 220 delivery system).
Version: app **3.0.61+132**, backend migration **60**. Reference: `ANALYSIS_220.md`, `TODO_220.md` (cards, boxes, receipts, Pending).

---

## 0. Your decisions that apply to 221

| # | Decision |
|---|---|
| D1 | The server keeps a message only until it is delivered (+7 days); it works like a postman. |
| D2 | The admin gets chats **from the phones** (Owner only). |
| D7 | Half copied, then the link breaks → the carrier says "I carry parts 1-40 of 80", never "stop". Only the admin's phone or the server sends the real stop. Nothing is lost if a carrier never comes back. |
| D8 | Get chat: **Full chat · Last 7 days · Last 30 days · Choose dates**; +1 day both sides (phone clock); extra messages greyed "Outside the dates (phone clock)". |
| D9 | User deletes are **soft** (hidden on the phone). Only the admin's **Delete completely** removes everywhere: one message · full chat · last 7 / 30 days · dates; red confirm with count. |
| D10 | Hidden text kept forever; hidden photo / video / file deleted after **60 days**, thumbnail kept. |
| D11 | Users see nothing (Audit log only). |
| Q4 | Half an answer can be read: "40 of 80 parts, the rest is on the way". |
| Q6 | Owner only · up to 20 MB of others' answers per phone · web "New request: pick 2 people / a group". |

## 1. What 221 delivers (user view)

**Owner (web + app):**
- **Conversations**: list of chats the server has seen (who, kind, count, last time; no text) + **New request** (pick 2 people or a group, also chats the server never saw). (mocks web01, app01)
- **Request sheet**: Full chat / 7 days / 30 days / Dates → card goes to all phones; row shows **Pending**, then **40 of 80 parts**, then ready. (app02)
- **Viewer**: route tag (Server / Wi-Fi Direct / Mesh), grey **Deleted by user · date**, greyed **Outside the dates (phone clock)**, thumbnails + **Get full photo**, the parts bar, **Close** (copy deleted now; else 24 h after first read). (web03, app03)
- **Delete completely** (message / full / 7 / 30 days / dates) → card to every phone of the chat; Admin actions shows "3 of 5 phones".
- All of it also appears in **Admin actions** (220).

**Users:** deleting a message / clearing a chat / leaving a group looks exactly the same as today. Nothing else visible.

**Server owner:** the database stops growing with old chats; a clean-up runs and the file shrinks at night.

## 2. Server postman (221a)

### 2.1 How long the server keeps messages (code facts: no message clean-up exists today; `CleanupSecurityArtifacts` `security.go:213`; sync skips missing rows `sync.go:104-118`)
| Row | Deleted when |
|---|---|
| 1-to-1 delivered | 7 days after `delivered_at` (sender's offline ✓✓ comes via sync from this row, `messaging.go:133 markDelivered`; 208 auto-download window) |
| 1-to-1 not delivered | 60 days after `created_at` |
| group message | 7 days after every device with a `group_message` sync event has `group_receipts.delivered_at`; at most 60 days |
| with them | `message_attachments` / `group_message_attachments`, reactions, receipts, matching `sync_events`, `files` rows + disk files no longer used (existing orphan rule `attachments.go:732`) |
| old rows already there | same rules → the first run frees the space |

- New `internal/httpapi/postman_221.go`: `CleanupMessages221(now)`; batches of 500 per short transaction (`SetMaxOpenConns(1)`: close rows before next query, never `a.store.DB()` inside a tx); returns counts.
- Called next to `CleanupSecurityArtifacts` at start (`cmd/server/main.go:214`) and on the same ticker (`:236`).
- **Night compaction**: at the first ticker after 03:00 server time, `VACUUM` only when freed ≥ 50 MB or ≥ 20 % since the last one (kept in `admin_settings` key `postman_last_vacuum`). Not during a backup (`admin.go:463 VACUUM INTO` uses the same lock).
- Logs: `POSTMAN_CLEAN direct=N group=N files=N freedMB=N took=Nms`, `POSTMAN_VACUUM before=NMB after=NMB took=Ns`.
- Test clock: `LOCALLINK_POSTMAN_NOW` (test env only) to move time in tests.

### 2.2 Chat list without text (`conv_index`, migration 60)
```
conv_index(kind TEXT, a TEXT, b TEXT, group_id TEXT, first_at, last_at, count INT,
           PRIMARY KEY(kind, a, b, group_id))      -- a < b (device ids), group rows: a=b=''
```
- Filled once in the migration from `messages` / `group_messages` (before any clean-up).
- +1 on every new message: in `insertMessageAt` (`messaging.go:46`) and the group insert (`groups.go`, `CreateGroupMessage`) — one added call each, inside their existing transaction.
- Never deleted by the postman (it is what the Owner picks from).
- `AdminConversations` (`admin_conversations.go:177`) reads `conv_index` + names (same JSON fields + `on_server` count).
- `AdminConversation` / `AdminGroupConversation` / `AdminConversationMedia` (`:317, :419, :514`) **stay** (old tests; they show what is still on the server).

## 3. Get chat (221b) — on top of 220

### 3.1 The request card (220 card, new actions)
- `get_chat`: boxes for every target phone: 1-to-1 = all phones of both people; group = all member phones (server: `group_members`; offline app: local `group_members` table). Box detail: `{chat:{kind,a,b|group_id}, from, to, plus_day:true}` (from/to empty = full).
- `get_file`: `{file_id, message_id}` → boxes for phones that may have it (the 2 people / members).
- `answer_ack`: the stop for data answers (server- or admin-signed): `{answer_id, have:[part ranges], all:true|false}`.
- Owner only (`read_conversations`); offline app: owner `AdminCert` only.
- `need` = 1 answer (the first complete answer finishes it; other target phones that see the progress / stop don't start; a phone halfway stops).

### 3.2 Target phone builds the answer (once)
- Reads **live + hidden** messages of that chat in the range (`created_at` between from-1 day and to+1 day; index `idx_messages_conversation` has `created_at`; groups filter on `group_messages.created_at`).
- Each message: `id, sender, created_at, body, route (messages.channel: server / direct / mesh / ''), status, reactions, deleted_by_user_at (hidden), outside (true when only in the +1 day margin), attachments [{file_id, name, type, size, thumb ≤ 8 KB}]`. Hidden files older than 60 days: `file_gone:true`.
- Group messages have no `channel` column → route `''` (shown as no tag). (Code fact: `local_store.dart:309`.)
- JSON → gzip → locked for the admin's `reply_key` (llaa1) → split into **parts of 32 KB**, numbered `1..N`; each part signed by the target phone: `{answer_id, action_id, part, of, sha256, data}`.
- Max 5 MB by phones (text first, then thumbnails; if over 5 MB: thumbnails of the oldest dropped, `trimmed:true`). Over the server: max 20 MB.
- Kept on the target until the stop (`answer_ack all:true`).

### 3.3 Travel and the stop
- Online: the target uploads parts to `POST /api/v1/admin-actions/answers` (resumable: server replies which parts it has).
- Offline: frames `admin_act_parts` (new one-step type, added in 221 to `MeshOneStep`) to linked phones; carriers keep parts (≤ 20 MB of others' answers per phone; full → drop thumbnails parts of the oldest answer first, text parts never), pass them to the next phones and to the server when online.
- **Progress note** (carrier, signed): `{answer_id, have:"1-40", of:80}` spreads like a receipt; Admin actions shows "40 of 80 parts" only when the admin's phone / server confirms them (`answer_ack have`).
- Next phone meeting the target asks only for parts the admin side doesn't have; a part someone carries but hasn't delivered may be copied again (joined by number; duplicates dropped by sha256).
- **Stop** = `answer_ack all:true` from the server (when it has all parts) or the admin's phone. Every phone deletes its parts and keeps a tombstone.
- The server keeps answer parts **24 h after the Owner first opens it**, or until **Close**; then deletes them (`admin_action_answers`).

### 3.4 Full photo / video
- Viewer "Get full photo" → `get_file` card → the phone that has it uploads (server) or sends on a **direct** link only (Wi-Fi Direct / same router), ≤ 50 MB (204 limit), locked for the admin.
- Hidden file older than 60 days → "File removed after 60 days · thumbnail only".

### 3.5 Old phones
- Target build < 132 → "Pending · old app" (they can't answer Get chat; they still do 220 actions if ≥ 131).

## 4. Soft delete (221c, app DB 35 → 36)

### 4.1 Tables
```
hidden_messages        (same columns as messages + hidden_at, hidden_how TEXT)        -- me | everyone | clear | peer
hidden_group_messages  (same columns as group_messages + hidden_at, hidden_how)       -- me | everyone | clear | exit | group_deleted
hidden_attachments     (message_id, kind 'direct'|'group', file_id, name, mime, size, local_path, thumb_path, file_gone_at)
hidden_reactions       (message_id, kind, reactor, emoji, at)
purged_ids             (id PK, at)     -- ids deleted completely by admin (60 days), to drop late copies
```

### 4.2 Where (code facts: `local_store.dart`)
- `deleteMessageLocal` (`:2841`): **+2 lines**: first `await _hide221.copyDirect(id, how)` (row, attachments, reactions → hidden tables); at the end `return _hide221.keepFiles(paths)` (removes the full file + thumbnail paths from the list the caller deletes — the callers `message_delete_actions.dart:26-27 deleteFiles` delete whatever is returned).
- `deleteGroupMessageLocal` (`:2861`): same 2 lines.
- `deleteGroupLocal` (`:3715`, exit / delete group): same 2 lines (all messages of the group).
- Clear chat uses `clearableDirectMessageIds` / `clearableGroupMessageIds` (`:2893, :2911`) → `deleteMessageLocal` per id → covered.
- A received "delete for everyone" calls the same functions → soft on this phone too.
- `hidden_how` comes from a small context value set by the callers' existing code path (default `me`); no caller signature changes.
- New file `lib/core/services/hidden_store_221.dart` (`_hide221`): copy, keepFiles, 60-day sweep (daily, on app start + every 24 h: delete full files older than 60 days, set `file_gone_at`, keep thumbnails), purge (§5).
- Nothing reads the hidden tables except Get chat and Delete completely → all 53 existing message reads unchanged.

### 4.3 Not possible
Uninstall, Clear storage, factory reset remove hidden copies. Phones < 3.0.61 delete for real.

## 5. Delete completely (221d)

- `delete_completely` card (Owner): box detail `{chat, scope: "message"|"full"|"range", ids?, from?, to?}`; targets = every phone of the chat (both people / all members); `need` = number of phones ("3 of 5").
- Server first deletes its own copies (messages / group_messages / files / answers / conv_index count adjusted) and audits `admin_purge`.
- Target phone: deletes matching rows from **live and hidden** tables + attachments + files + thumbnails (real delete, `File.delete`), removes outbox rows (`pending_attachments`, `group_outbox`, `pending_group_attachments`), calls `LocalLinkNotificationService.handleMessageDeleted(id)` (`local_link_notification_service.dart:175`), adds ids to `purged_ids` (60 days) → receipt `{removed:N}`.
- Late copies (a message still in a mesh queue): a listener on the existing public streams `messaging.incoming` / `groupMessagingService.incoming` (`reliable_messaging_service.dart:413`, `group_messaging_service.dart:634`) deletes any id in `purged_ids` right after it arrives (no change to receive code).
- Confirm (web + app): radio list with counts from the current copy (or "count unknown — no copy yet" when the Owner hasn't fetched the chat), red button "Delete N messages completely".

## 6. Screens

### 6.1 App (Owner)
- `features/admin/presentation/screens/admin_conversations_screen.dart` (app01): online list from `GET /api/v1/admin/conversations`; offline list from the phone's own known chats + directory; rows "Ask phones" / status; **New request**.
- `admin_chat_request_sheet.dart` (app02): Private chat / Group, people pickers (directory), group picker, range chips + date picker, Send.
- `admin_chat_viewer_screen.dart` (app03): bubbles, tags, parts bar, Get full photo, long-press → Copy text / Delete completely, top menu → Delete (range sheet) / Close.
- Copies on the admin phone: encrypted at rest with the existing store, deleted 24 h after first open or on Close.
- `admin_panel_screen.dart`: one more row **Conversations** (Owner only).

### 6.2 Web (in the backend zip)
- `views.js` Conversations (`id: 'conversations'`, `:1451`): rework to list + New request + request sheet + viewer + delete sheet (mocks web01, web03). Old Conversations code stays reachable only through the old endpoints for tests; the page uses the new flow.
- Privacy page (`views.js:1833`): "The server keeps a message only until it is delivered (+7 days). The Owner can ask the phones for a chat; it is written to the Audit log."
- Dashboard: no Storage card (you skipped it).

## 7. Backend (bundle 221) — files
- `store.go`: migration 60: `conv_index` (+ fill), `admin_action_answers(answer_id, action_id, part, of, sha256, data BLOB, from_device, at, PRIMARY KEY(answer_id, part))`, `admin_action_answer_meta(answer_id PK, action_id, of, first_opened_at, closed_at)`.
- New: `postman_221.go`, `admin_chat_221.go` (get_chat / get_file / delete_completely cards, answers upload, parts ack, Close, purge of server copies), tests `postman_221_test.go`, `admin_chat_221_test.go`.
- `main.go`: routes `POST /api/v1/admin/chat-requests` (makes the card), `GET /api/v1/admin/chat-requests/{id}/answer?part=`, `POST /api/v1/admin/chat-requests/{id}/close`, `POST /api/v1/admin/chat-requests/{id}/file`, `POST /api/v1/admin/purge`, `POST /api/v1/admin-actions/answers` (phones upload parts) + clean-up line.
- `messaging.go` (`insertMessageAt`) and group insert: +1 `conv_index` call each.
- `admin_conversations.go` (`AdminConversations`): list from `conv_index`.
- Web files: `views.js`, `app.js`, `components.js`, `styles.css`.

## 8. App (bundle 221) — files
- DB 36: `database_config.dart`, `local_store.dart` (tables + the 3 × 2 lines).
- New: `core/services/hidden_store_221.dart`; `features/admin/data/actions/chat_answer_builder.dart` (range, live + hidden, parts), `chat_answer_carrier.dart` (parts store, cap 20 MB, progress), `chat_answer_reader.dart` (admin side: join, verify, open, read half), `delete_completely_doer.dart`; the 3 screens in §6.1.
- `admin_action_doers.dart` (220): 3 new cases call the new files.
- `MeshBlockPolicy.kt`: + `ADMIN_ACT_PARTS`.
- `app_build.dart`: 132 / '3.0.61'.
- `admin_panel_screen.dart`: + Conversations row.

## 9. Tests
**Backend:** 1. 1-to-1 delivered kept at 6 days, gone at 8; undelivered kept at 59, gone at 61. 2. Group: kept while a receipt is missing (< 60 days), gone 7 days after the last receipt, gone at 61 anyway. 3. Files: removed from disk with the last message using them; shared file stays. 4. Sync after clean-up = 200, skips missing rows; sender ✓✓ still arrives within 7 days. 5. VACUUM rule (50 MB / 20 %), not during backup. 6. `conv_index` filled at migration, +1 per message, survives clean-up. 7. get_chat card: Owner only (operator / auditor 403), boxes for both people's phones / all members. 8. Answers: parts upload resumable, sha256 / signature checked, ack ranges, all → stop, 24 h after first open + Close delete. 9. Purge: server copies gone, card `need` = phones, receipts "3 of 5". 10. Old endpoints still pass old tests.
**App:** 1. Delete for me / everyone / clear chat / exit group / delete group → gone from every normal read (existing tests unchanged) and present in hidden tables; full file + thumbnail still on disk; `deleteFiles` gets the rest. 2. 60-day sweep removes full file only. 3. Answer builder: range + 1 day, `outside` flag, live + hidden, routes, `file_gone`, 5 MB trim. 4. Parts: split / join / sha256, carrier cap 20 MB (thumbs first), progress note, second carrier fills the rest, stop deletes parts. 5. Reader: half readable ("40 of 80"), duplicates dropped. 6. Delete completely: live + hidden + files + outbox + notification, `purged_ids` drops a late copy. 7. Screens: list, sheet, viewer, delete sheet render (mock match). 8. 220 tests + all old tests green.

## 10. Phone tests
- 221-1 Server clean-up (test clock): server rows gone; both phones still show the chat with ✓✓; web Diagnostics DB size drops after the night VACUUM.
- 221-2 Sumaira deletes 2 messages (for me) → Get chat (Full) → both shown "Deleted by user".
- 221-3 Get chat Last 7 days → only that week (+ greyed margin).
- 221-4 Both phones offline → Pending; Sumaira online → ready.
- 221-5 Offline Owner app (no server): request by Wi-Fi Direct; a 3rd phone carries; answer arrives.
- 221-6 Walk away halfway → "40 of 80 parts" (half readable) → another meeting finishes it.
- 221-7 Get full photo (online and on a direct link).
- 221-8 Delete completely by range → gone on both phones, also from a new Get chat.
- 221-9 Exit a group → messages hidden; Get chat for that group still shows them.
- 221-10 Users see nothing during 221-2..8.
- 221-11 Old phone (3.0.60) → "Pending · old app" for Get chat.

## 11. Delivery
- Backend zip first (migration 60, postman, web Conversations), deploy; then app zip (3.0.61+132).
- Commits: `v3 bundle 221 backend: server keeps messages only until delivered, chat list without text, owner gets chats from phones, delete completely` · `v3 3.0.61+132 bundle 221: chats from phones (full or dates, parts over every link), soft delete for users, delete completely`.
- APPLY_221.md: the 3 × 2 added lines in `local_store.dart`, every touched file; first clean-up may take a minute on a big database (logged).
