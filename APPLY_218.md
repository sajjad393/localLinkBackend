# APPLY 218 – backend (218a: offline account joins the server)

Built on top of backend 217 (`6c82546`, already pushed). **Order: this zip → commit → deploy → then the 218 app.**
Old apps keep working: every new field is optional and every old error text is unchanged.

## What changed

**Database (migration 58, automatic on start)**
- New table `offline_ids`: phone-only id (`offline_` + 12 hex) → real account, phone id, date. Lets friends' phones follow her to the real account.

**Join (register / login)**
- The app may send `offline_user_id` and (register only) `recovery_code`.
  - A wrong-looking offline id is ignored (never an error).
  - The recovery code the phone already shows is kept by the server (format `XXXXX-XXXXX-XXXXX-XXXXX`); a wrong-looking code is replaced by a new one, as before.
- The link is saved in the same step as the account. If the same offline id is linked again, the first account keeps it.
- Audit: `account_from_offline` (new account) / `account_link_offline` (signed in to her existing account).

**Error codes** – the answer keeps the old `error` text and adds `error_code`:
- `USERNAME_TAKEN` (name used by someone else), `DEVICE_TAKEN` (this phone id belongs to another account; the answer adds `owner` = that account's @name **only** when the saved hardware fingerprint of that phone equals the one sent now, so a stranger never learns a name), `PAIRING_REQUIRED`, `BAD_LOGIN` (wrong name or password), `PHONE_TAKEN` (profile number used by another account), `NOT_ON_SERVER` (add contact: no such account yet).

**Block made while offline follows her**
- If the newest block record for her offline id (or for her phone id) is a block, the server blocks the real account too ("auto: blocked while offline"), unless the real account already has a newer record or the newest one is an unblock. Audit `admin_block_carry`.

**New route `POST /api/v1/users/resolve`**
- Signed-in phones send up to 100 ids (offline ids or phone ids, max 128 chars each) and get back `{found:[{id,user_id,username,device_id}]}`. Unknown ids are left out. Limit 30 calls per minute per phone (429 + Retry-After).

**Contacts / profile**
- Add contact by an offline id → saved on the real account; the answer now has `user_id`.
- Profile with a number already used: 409 `PHONE_TAKEN` and Audit `profile_phone_conflict number=***4567` (last 4 digits only).

**Web panel**
- Manage person: "Joined from offline · 5 Nov 2026" tag when the account came from a phone-only account (`joined_offline_at` in the users list).

## Files
- `internal/store/store.go` (migration 58)
- `internal/httpapi/api.go` (`writeErrorCode`), `auth.go`, `admin_blocks.go`, `contacts.go`, `profile.go`, `admin_panel.go`, `admin_216.go`
- new `internal/httpapi/users_resolve_218.go`
- `cmd/server/main.go` (1 route)
- `internal/httpapi/adminweb/views.js`, `styles.css` (tag; these 2 files also carry the 217 changes)
- new `internal/httpapi/offline_join_218_test.go` (13 tests)
- `APPLY_218.md`, `TODO_218.md`

## Checked here
- `go test ./...` green (all old tests unchanged + 13 new).

## PowerShell (backend)
```
cd <LocalLinkBackend folder>
git pull
Expand-Archive -Force "$HOME\Downloads\locallink_backend_218_2026-11-05.zip" .
git add -A
git add -f APPLY_218.md TODO_218.md
git commit -m "v3 bundle 218 backend: offline account joins server (offline_ids, kept recovery code, error codes, users resolve, block carry-over, phone taken audit)"
if ($LASTEXITCODE -eq 0) { git push }
```
Then deploy the server as usual (migration 58 runs by itself). Hard refresh the panel once (Ctrl+F5).
