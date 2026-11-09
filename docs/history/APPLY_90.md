# Bundle 90 (backend) — account_contacts.is_private (default public)

Per-contact privacy flag, lister-owned:
- `account_contacts` gains `is_private INTEGER NOT NULL DEFAULT 0` via the
  existing ensureColumn migration (runs on open, idempotent).
- `POST /api/v1/account/contacts` accepts OPTIONAL `is_private` (bool):
  omitted → old behavior (INSERT OR IGNORE, flag untouched);
  present → UPSERT toggles the flag on the caller's own row.
  The reciprocal row (mutual pair) is always created PUBLIC — one side may
  never impose privacy on the other side's entry.
- `GET /api/v1/account/contacts` returns `is_private` (omitempty — old
  clients' JSON parsing stays byte-compatible).
- The app's mesh layer uses the flag to stop re-sharing private contacts in
  sync responses; the server keeps it so reinstall restores it.

## Files
- internal/store/store.go (schema/migration)
- internal/httpapi/contacts.go (handler + model)
- internal/httpapi/contacts_private90_test.go (new; default/toggle/reciprocal)

## Apply
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git checkout main; git pull origin main
$zb = Get-ChildItem "C:\Users\sajjad Ahmed\Downloads\*.zip" |
      Where-Object Name -like "locallink_backend_fixes90*" |
      Sort-Object LastWriteTime -Descending | Select-Object -First 1
Expand-Archive -LiteralPath $zb.FullName -DestinationPath . -Force
go test ./... -count=1      # FULL suite green when run with a clean module cache
go build ./...
git add -A
git commit -m "contacts 90: per-contact is_private flag (default public) — lister-owned, optional upsert toggle, reciprocal stays public"
git push origin main
```

## Verified on my side (this build)
`go test ./... -count=1` → ok (httpapi 1.905s; store; ws) with a FRESH module
cache. If your first `go test` errors with
"no required module provides package modernc.org/sqlite", the Go module cache
has stale entries — blowing it away fixes it:
`go clean -modcache` (or just the modernc.org entries), then re-run.

## Verify endpoints
```
POST contacts {user_id: X}              → list: X, is_private absent/false
POST contacts {user_id: X, is_private: true}  → list: true (existing added_at kept)
GET contacts (as X's account)           → reciprocal entry exists, still public
POST contacts {user_id: X, is_private: false} → list: false
```
