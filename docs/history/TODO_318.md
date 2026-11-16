# TODO 318 - Get chat again: only what is new (no duplicate text / files)

Status: FREEZE ON - plan only, build only on "Build". Checked against app `5bad575` + 317 work patch, backend `6db45b6` + 317.
User picks 16 Nov: keep the copy **7 days** on the Owner phone (locked) · sheet = **Open it + Get new messages only**,
"Get all again" under More · **web stays full** · **fix the 24 h sweep** in the same build.

## Today (code facts)
| # | What | Code |
|---|---|---|
| 1 | Every get_chat rebuilds the whole range from zero; the phone does not know what the admin has | `chat_answer_builder.dart` `buildChat` l.444, `messages()` l.324 |
| 2 | 317 file queue looks only at files of the SAME request -> a 2nd request asks every photo / video / voice again | `file_queue_317.dart` l.338 `files[r.id]` |
| 3 | 314 blocks only an open (Sent / Reached / Coming) request; a Ready one does not block | backend `admin_chat_314.go` `openRequest314` l.257 |
| 4 | No new messages -> the whole chat is sent again, card Sent -> Coming -> Ready + buzz | builder; no "nothing new" answer exists |
| 5 | Owner phone online: every open fetches the answer from the server; no local copy | `chat_answer_reader.dart` `open()` l.1058, `fileBytes` l.1105; viewer `_bytes317` is memory only |
| 6 | Server deletes answer copies 24 h after first open / at Close / 30 days unopened | backend `admin_chat_221.go` l.36-37, `CleanupAnswers221` |
| 7 | Phone sweep (24 h after open) remembers opens only in memory -> after an app restart offline copies stay until Close (BUG) | `chat_answer_reader.dart` `_openedAt` l.245, `sweep()` l.359; timer `app_dependencies.dart` l.1293 |
| 8 | Online request detail is built by the server from fixed fields (chat, from, to, plus_day) -> a new field needs a backend change | backend `admin_chat_221.go` l.224 struct, l.367 |
| 9 | Range margin: the phone already adds 1 day each side (wrong clocks) | `ChatRange221.place` |
| 10 | Owner phone database is SQLCipher (locked) | `hidden_store_221.dart` l.15 |

## Plan
- **K1 Kept copy (Owner phone, 7 days).** When an answer is complete and opened, save it locally (messages JSON +
  range + `got_at` + answering phone + newest `created_at` + per-day counts) in the SQLCipher DB; files that are here
  saved too (bytes). Goes 7 days after first open, at Close, or at Delete completely of that chat. Opening a request
  whose server copy is gone uses the kept copy. Storage cap for kept files (proposed 300 MB, oldest copy goes first) -
  confirm before build.
- **K2 Sweep fix (bug #7).** First-open time saved in the DB (not memory); sweep runs at start + every 6 h; rule
  7 days (K1). Server rule unchanged (24 h - the phone keeps its own copy).
- **K3 Sheet.** Picking a chat + range that a kept copy covers shows "Already here · full chat, got 10:05 today"
  with **Open it** and **Get new messages only** (main button); **Get all again** under More. A range inside the kept
  copy (7 days inside full) opens at once - no card, no phone asked. 314 already_asked unchanged.
- **K4 New-only card.** Detail adds `since` = newest `created_at` in the kept copy minus 1 day (+ `since_n` = count
  in the copy). Server: accept + pass `since` (backend small: struct + detail l.367; web never sends it). Offline path:
  same field in `svc.make` detail. Old app on the answering phone ignores `since` -> sends all (merge still works).
- **K5 Answering phone.** With `since`: messages `created_at >= since` + older messages with `hidden_at >= since`
  (deleted later) + reactions of messages in the last day; part 1 also carries per-day counts of the whole range
  (tiny). Nothing new -> ONE small part `up_to_date: true` (no messages) - not "No messages / empty".
- **K6 Merge (Owner phone).** By message id into the kept copy (live row wins, deleted flag kept); a message missing
  on the new phone stays (history). Per-day counts differ (late mesh messages with an old time) -> ask THAT day only,
  once, by itself. Delete completely by the Owner removes those ids from the kept copy.
- **K7 Card + viewer.** Up to date -> "No new messages since 10:05", green, NO buzz, closes like Ready. New ones ->
  "Chat ready · 12 new" (one buzz). Viewer shows one chat with the WhatsApp-style "New messages" bar.
- **K8 Files once.** Before asking a file, the queue looks in every kept copy + every live request: here -> reuse
  (no card); on the way in another request -> wait for it. Voice notes the same.
- **Web:** stays full (no server-side chat storage). The web shows a new-only request as a normal request.

## Not changing
Sealing / SecurityPolicy, 314 rules, 317 trips / 8 MB cap, server 24 h rule, mesh voice, video (stopped).

## Tests
- builder: `since` -> only new + deleted-later + last-day reactions; nothing new -> 1 part `up_to_date`; no `since` -> same bytes as today.
- merge: by id, no duplicates, deleted flag, day-count mismatch asks that day once; purge removes ids.
- kept copy: saved on open, used when the server copy is gone, gone after 7 days / Close / Delete completely.
- sweep: first-open time survives a restart (bug #7 proof: fails on old code).
- sheet: covered range -> "Already here", Open it / Get new messages only, Get all again under More; inside range -> no card.
- file queue: file here in another request -> no card; on the way -> waits; voice same.
- card: up to date = green, no buzz; "12 new" = one buzz.
- backend: `since` accepted + passed; web request unchanged; old tests green.
- Regression: 221 / 307 / 310 / 313 / 314 / 317 tests, full suite, Kotlin, analyze, go test.

## Limits (tell user)
- Saving works only while the kept copy is there (7 days). Web owner: always full.
- An old LinkUp on the answering phone sends everything (still merged, no duplicates shown).
- Kept files use storage on the Owner phone (cap above).

---

## As built (3.0.109+180, 16 Nov) - user said "Build" after the mock

**File cap: 300 MB of kept files on the Owner phone (ASSUMED - the question was never answered).** Oldest kept
file goes first when over the cap.

| Point | Built |
|---|---|
| K1 | `Kept318` + `SqlKeptStore318` (new tables in the locked SQLCipher DB, text in 256 KB rows, files in 512 KB rows). A complete full answer opened on the Owner phone becomes the copy of its chat (an older copy of the same chat is replaced; its files move to the new copy). Goes at 7 days after first open, Close, or Delete completely (range/full); Delete completely of messages removes those ids. |
| K2 | Reader first-open times saved in settings (`answer_opened_318`); answer parts kept 7 days after first open (was 24 h). Sweep at start + every 6 h (`sweepEvery318`), also sweeps kept copies. **Bug fixed:** open times were memory-only, so after a restart nothing was swept. |
| K3 | Sheet: green "Already here" box. Same range = big "Get new messages only" + "Open it" + More (Get all again). Range inside the copy = "Open it" only, "No card, no phone is asked." (opens locally). |
| K4 | `requestNew318`: `since` (newest - 1 day), `since_n`, `since_h`. No copy = normal request. Web never sends it. Backend passes them into the card detail; a bad `since` is dropped. |
| K5 | Builder `_buildNew318`: messages from `since`, older ones deleted after `since`, per-day counts (UTC). Same check + nothing deleted later = ONE `up_to_date` part. |
| K6 | Merge by id once per request (newer wins, deleted mark stays, thumbnails kept, history kept). A day with more messages on their phone = that day asked once by itself. |
| K7 | Card "Get chat · new only": Asking for new messages / Up to date (green, no buzz, auto-cancel) / Ready · 12 new (one buzz). Footer "Your copy: full chat · got … · kept until …". Chips Up to date / 12 new + New only; full request "Kept · 7 days". Viewer header "+12 new today 10:43" and the "12 new messages" bar (1 = "New message"), scrolled to it. |
| K8 | `fileBytes` reads kept bytes first; a fetched file is kept with its copy. Queue: a kept file = here (never asked); a file here/on the way in another request is reused; a new-only request plans only its new messages; files that are here get kept once (one at a time). Viewer shows kept files as here. |

Found while testing: `open()` of a new-only request hung forever (`whenComplete(() => map.remove(id))` returned
the same Future, which waited on itself). Fixed with a block body; test `bundle318` covers it.
