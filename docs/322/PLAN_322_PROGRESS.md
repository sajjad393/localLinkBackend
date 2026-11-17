# Bundle 322 — build progress (app + backend + web)

**Branch:** `b322` in `project/app` (base `faf9080` = 3.0.112+183 / bundle 321)
**Versions now:** app `3.0.113+184`, backend 322 (web page + API)
**Patches:** `~/ws/wip_322/app.patch` (12 files), `~/ws/wip_322/be.patch` (8 files)
**Suites:** app `flutter test` → **3425 passed, 0 failed** · backend `go test ./...` → **7/7 packages ok**
**Zips:** `~/locallink_322_2026-11-17.zip` → `locallink_app_322_*` + `locallink_backend_322_*`

---

## What is BUILT and PROVEN

| Wave | Item | What changed | Proof |
|---|---|---|---|
| W1 | **S1** | A file with a Get-file row on the server now shows `coming`, not `next` (`cardMeansComing322`). 321 showed `next` while the card was live, so the 321 10-min timer never ran on it — the 17 Nov "Group 1 · 6/8" card. | `bundle322_files_test` group S1 (3 tests) |
| W1 | **S2** | A failed ask releases its slot and retries with 1 → 2 → 5 → 10 min backoff, max 5 tries, then `gone`. 321 kept the slot and `break`-ed out of the ask loop: 2 failures pinned the 2-file window forever. Counter is persisted (`fails322` in the plan), reset only when the file really moves. | group S2 — incl. `why=ask_failed tries=5` |
| W1 | **S3** | A file the other phone stopped (row `closed`) counts as `gone`, not "still wanted". 321 left it `stopped`, which kept `wanted` at 8/8 so the card never ended. | group S3 |
| W2 | **S5** | Backstop: a files card whose `here`/`coming` do not move for `cardStuckAfter322` (30 min) is cancelled with `CHAT_REQ_CARD_307 cancel reason=stuck30 … missing=N`; it stays away until the numbers move again. | `bundle322_card_test` group S5 (4 tests) |
| W2 | **S6** | `FileTrip317.swipeAfter322 = 10 min` (was 30) — a card that is not moving can always be swiped away. | groups S6 |
| W2 | **S7** | Every card change logs `CHAT_REQ_CARD_307 show id=<last6> step=… here=…/… buzz=…` and every drop `drop id=… reason=opened|closed|done|gone|empty|cancelled`. No names, no full ids, ≤ 1 line/s. **This is why the bug was invisible: the 12:00 logs had zero `CHAT_REQ_CARD_307` lines while a card was on screen.** | group S7 (4 tests) |
| W3 | **G1** | A permanent server answer (`already belongs to different message`, `body is reserved`, `invalid … created_at`, `SENDER_ID_MISMATCH`, …) removes the row, marks the bubble failed, logs `GROUP_SEND_PERMANENT`. 321 put it back to `queued` → 687 sends/hour on the TECNO. | `bundle322_group_outbox_test` G1 |
| W3 | **G2** | `groupOutboxPending322()` skips `failed / delivered / server_accepted / refused`; only `flush()` + the hand-off switch to it. `groupOutbox()` unchanged. | G2 |
| W3 | **G3** | The locked body is kept per id and re-sent **unchanged** (`lockCache322`), cleared on ack. A fresh nonce per attempt is exactly what made the server say "id already belongs to different message". | G3 |
| W3 | **G5** | `retryBackoff322` 5 → 10 → 20 → 40 → 60 s instead of a flat 5 s; first attempt still immediate. Logs `GROUP_RETRY n=… next_s=…`. | G5 |

**Rollback switches (all named, all one-line):** `cardMeansComing322=false` (S1),
`maxAskTries322=0` (S2), `cardStuckAfter322=Duration.zero` (S5),
`swipeAfter322=Duration(hours:1)` (S6), `permanentCode322=false` (G1),
`lockCache322=false` (G3), `retryBackoff322` (G5).

---

## Decisions taken while building (need your OK)

1. **S1 was re-scoped, the plan's timer was not shipped.** The plan wanted a new "`next` clock". Building it showed two timers fighting over one file: the new one re-asked a file that the 321 timer then declared `stuck_twice` → `gone`, and it broke `bundle321_files_test` ("a file that moves is never re-asked"). The real cause was a **state mapping** bug (a live card shown as `next`), so S1 is now that mapping and the existing 321 timer (already tested) does the timing. Fewer moving parts, no second timer, 321 file tests untouched.
2. **G4 (keep the sender key while a row is queued) is WITHDRAWN.** It contradicts a security rule that `groups_g6_sender_keys_test` enforces and you approved: *a member leaving makes a new key* (otherwise the person who left keeps reading). G3 already removes the reason G4 existed (one body per id), so nothing is lost.
3. **S8 (group name follows a rename) is NOT built** — it is a backend change (`admin_chat_221.go`, additive `label_current`). Backend 322 is untouched so far, per the agreed order.
4. Text for the ended card reuses the words that already exist ("N files not available"); **no new UI, no new strings**.

---

## W4 — web App version auto-pick (BUILT)

| Item | What it does |
|---|---|
| V1 | `GET /api/v1/admin/version-rule` also returns `uploads` + `newest_upload` (additive; `rule` / `builds` untouched) |
| V2 | Version name + Build filled from the newest upload, **read-only**; with no upload they stay editable exactly as 321 |
| V3 | "Use this build" fills both boxes — **Save rule stays a tap** |
| V4 | "Pick another upload…" sheet (newest first, Esc closes), "Upload a new APK" jumps to App download |
| V5 | `require_upload322` (opt-in): a build that is not on the server is refused with *"that build is not on this server - upload it first"*. Callers that don't send the flag behave exactly as 321 |
| V6 | One state line: green "the rule is the newest upload" / amber "a newer upload is here … the rule is still build N" / grey "no APK yet" |
| V7 | `version_rule_322_test.go` (3), `admin_web_322_test.go` (3), `testdata/appversion_322_smoke.js` — runs the **real** `views.js` in node against 6 server states |

## W5 — the overlap fix (BUILT) — this one was real and affected every page

At a 390 px window **the whole panel scrolled sideways**: `.section` measured
792 px inside a 390 px viewport. Three containers are CSS grids whose implicit
column is **max-content**, so the Phones-by-build table pulled every parent
wider. Fixed with `grid-template-columns: minmax(0,1fr)` on `.content-inner`,
`.section` and `.field`; the table now scrolls inside its own `.table-wrap`.
The amber "state" sentence was also inside a `white-space:nowrap` badge — it is
now a normal wrapping line.

**Proof (real files, not the mock):**

| Run | Result |
|---|---|
| `tools/real_page_audit.py` — real page, 6 states × 7 widths (390-1600) × 2 themes | **PASS 84 / FAIL 0** |
| `tools/audit_all_views.py` — 11 other pages × 3 widths × 2 themes | **PASS 66 / FAIL 0** |

Both read `project/be` directly, so a pass is a pass on the shipped code.

## Still to do

- **W6** housekeeping / decisions (N1–N6).
- **S8** (a renamed group still shows the old name) — backend, needs your OK:
   it changes the shape of the chat-request list the app also reads.
- **Phone verification** of W1–W3 with the log lines below, then the APK.

### What to look for on a real phone (W1–W3)
- A files card that stops moving: `FILE_QUEUE_GONE why=stuck_next|why=stopped|why=ask_failed`,
  then `CHAT_REQ_CARD_307 cancel reason=stuck30` at 30 min at the latest — **the card always ends**.
- Swipe a card after 10 min of no movement (it used to need 30).
- Every card: `CHAT_REQ_CARD_307 show …` / `drop … reason=…` lines — no more silence.
- A group message the server already has: **one** `GROUP_SEND_PERMANENT`, then nothing (it was 687 sends/hour).
