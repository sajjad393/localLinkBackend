# Bundle 89 — Backend (apply over current backend `main`)

## What this fixes
**IMPORTANT:** this zip contains FULL current files, including bundle 88c
(`auth.go` accepting `hardware_fp` on register). Apply it REGARDLESS of
whether 88c is already on your branch; the result is identical.

1. **Contacts are MUTUAL** (`internal/httpapi/contacts.go`)
   - `AddAccountContact`: writes the relationship **both directions**
     (`INSERT OR IGNORE`, keyed by the composite PK → repeated/add-reverse
     calls stay idempotent, no duplicates, still 200). The added user now sees
     the adder without manually re-adding.
   - `RemoveAccountContact`: deletes both directions — removal ends the
     relationship for both sides.
   - `ListAccountContacts`: **self-heals legacy one-way rows** — backfills the
     missing reverse row on read (idempotent), restoring visibility for pairs
     created before this change. This also feeds the restore manifest/data, so
     fresh-install restores return mutual lists too.
2. **Group member names are profile nicknames** (`internal/httpapi/groups.go`)
   - `GroupMembers` now joins `devices → profiles` and returns the profile
     display_name, falling back to the device label only when the account has
     no profile nickname at all. Fixes “My Android Phone (xxxx)” in group
     member lists.
3. **88c register fix (carry-over, unchanged since the 88c zip)**
   - `RegisterAccount` accepts + sanitizes + persists `hardware_fp`
     (`internal/httpapi/auth.go`) — decodeJSON rejects unknown fields, so 88-era
     clients that send it got `400 invalid request body` at REGISTER (login
     worked because that struct had the field).

## Tests (RUN in the sandbox, all green)
- `TestAccountContactsMutualLifecycle` — A adds B ⇒ B sees A (device id
  resolved live); B’s reverse add is a no-op (1 row each side); removal clears
  both sides.
- `TestAccountContactsListHealsLegacyOneWayRows` — legacy C→D row backfills
  D→C on list and persists it.
- `TestGroupMembersExposeProfileNicknameNotDeviceLabel` — nickname shown for
  both members' views; never the device label.
- Pre-existing `TestAccountContactsCRUDIdempotentAndRestored` — still PASS.
- `go build ./...` + full `go test ./...` (also under
  `LOCALLINK_REQUIRE_TLS=false`) — ALL PASS.

## Apply (PowerShell)
```powershell
Expand-Archive .\locallink_backend_fixes89_2026-10-23.zip -DestinationPath . -Force
git add -A
git commit -m "contacts: mutual server-backed relationships (reciprocal add/remove, legacy self-heal); group members show profile nickname; register accepts hardware_fp (88c carry)"
go build ./...
go test ./...
git pull origin main --rebase
go test ./...   # after rebase, re-verify
git push origin main
```
If the rebase stops on conflicts in these four files (your remote tip 6581d8b
is unknown to me), resolve by keeping **both** sides' handlers and ping me the
conflict — never `--force`.
