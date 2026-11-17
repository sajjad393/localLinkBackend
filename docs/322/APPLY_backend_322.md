# Bundle 322 — backend + web (backend 322)

Base: backend 321 = commit `54909a9`.
`be.patch` in the zip is `git diff` against that commit.

## How to apply

```bash
cd <your localLinkBackend repo>
git checkout -b b322 54909a9
git apply --binary /path/to/be.patch
go build ./...
go test ./...          # 7 packages ok
```

Then restart the server. The admin web files are embedded (`go:embed`), so the
new page ships inside the binary — **no separate web deploy**.

## What changed

### 1. App version page picks the build from what is on the server (W4)

| | |
|---|---|
| **V1** | `GET /api/v1/admin/version-rule` now also returns `uploads` (last 50 APKs, newest build first) and `newest_upload`. Each row: `id, version, build, file_name, size, sha256, added_at, added_by, downloads, current, is_rule, on_disk, phones_here, newest`. **Additive**: `rule` and `builds` are untouched, so an older web bundle behaves exactly as in 321. |
| **V2** | The page fills Version name + Build from the newest upload and makes them **read-only**. With **no** upload the two boxes stay editable, exactly as today (never a dead end). |
| **V3** | **Use this build** fills both boxes; **Save rule** stays a tap. Nothing is written without it. |
| **V4** | **Pick another upload…** opens a sheet of the APKs on the server (newest first, Esc / click-outside closes). **Upload a new APK** jumps to App download. |
| **V5** | `POST /api/v1/admin/version-rule` accepts a new optional key `require_upload322`. When the page sends it (only when the build came from a real upload) a build that is not on the server is refused: *"that build is not on this server - upload it first"*. Callers that don't send it are accepted exactly as in 321. |
| **V6** | One state line under the upload card: green "the rule is the newest upload", amber "a newer upload is here … the rule is still build N", grey "no APK yet". Wraps; it is a sentence, not a badge. |
| **V7** | Tests: `version_rule_322_test.go` (3), `admin_web_322_test.go` (3), `testdata/appversion_322_smoke.js` (runs the real `views.js` in node against 6 server states). |

### 2. Phone-width overlap fix (W5) — this affected every page

At a 390 px window the whole panel scrolled sideways (`.section` measured
792 px). Cause: three containers are CSS grids whose implicit column is
**max-content**, so one wide child (the Phones-by-build table) widened its
parents all the way up. Fixed with the standard one-liner on each:

```css
.content-inner { grid-template-columns: minmax(0, 1fr); }
.section       { grid-template-columns: minmax(0, 1fr); }
.field         { grid-template-columns: minmax(0, 1fr); }
```

Nothing else in those files changed. The table now scrolls inside its own
`.table-wrap`, as the approved mock does.

### 3. Proof it is really fixed (not "looks fine")

| Run | Result |
|---|---|
| `tools/real_page_audit.py` — the **real** page code, 6 server states × 7 widths (390…1600) × 2 themes | **PASS 84 / FAIL 0** |
| `tools/audit_all_views.py` — 11 other pages × 3 widths × 2 themes | **PASS 66 / FAIL 0** |
| `TestAdminWebSmoke322` (real views.js in node) | PASS |
| `go test ./...` | 7/7 packages ok |

Both scripts live in `~/ws/tools/` and read the files straight out of
`project/be`, so a pass is a pass on the shipped code.

## Not built here

- **S8** — a Get chat on a renamed group still shows the name it had when the
  request was made. The fix is additive (`label_current` via a `LEFT JOIN
  groups` in `admin_chat_221.go`), but it changes the shape of the chat-request
  list that the **app** also reads; it needs your OK before I touch it.
- No change to the version-rule signing, `seq`, `mode warn|stop`, `stop_days`
  0-30, or the offline rule phones carry.
