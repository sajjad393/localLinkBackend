# TODO 230 - audit S4: backend sign-in and password hardening (backend only)

Go ahead: 2026-11-09 ("Go ahead l will push later").
Base: backend `1f995eb`. No app change. No database migration.

## Your choices (ask_user 2026-11-09)
- Admin sign-in: **180 days, renewed on every use.**
- Admin web console token: **stays in localStorage** (S4.5 dropped).

## Parts

### S4.1 Admin sign-in ends after 180 unused days (audit X2)
- New `adminSessionIdle = 180 days` in `admin_auth.go`.
- `authorizeAdminSession` reads `last_seen_at`. More than 180 days ago = 401 (signed out).
- Every use already moves `last_seen_at`, so a session that keeps being used never ends.
- `expires_at` (10 years, bundle 211) is **not** changed:
  - the session row and the admin phone's offline key stay;
  - the old 211 tests stay green with no change.
- The admin phone refresh (`user_admins.go`) already moves `last_seen_at`, so the admin phone keeps working as before.
- Sessions list (`AdminOwnSessions`): an idle session shows as not active.
- An unreadable time is not treated as idle.

### S4.2 Devices without an account are off by default (found during S4)
- **Problem:** `POST /api/v1/devices` (open while `LOCALLINK_PAIRING_CODE` is empty) gave a token that never expires and has no account behind it. With it a caller could list every device, read identity keys, send messages, and use WS and sync.
- **Fix:**
  - `deviceOnlyAllowed()` is true only with `LOCALLINK_ALLOW_DEVICE_ONLY=true`, or with `LOCALLINK_ENV=test` (old tests).
  - `RegisterDevice` → 403 `DEVICE_ONLY_DISABLED`.
  - `authorizeDevice`: an old device-only token → 401. A log line `DEVICE_ONLY_REFUSED device=<8 chars>` is written at most once a minute.
- **App:** the LinkUp app never calls this route (`registerWithServer` has no callers). It signs up and logs in with an account (`/api/v1/auth/*`), and that path is unchanged.
- `.env.example`: adds `LOCALLINK_ALLOW_DEVICE_ONLY=false` with a note.

### S4.3 Constant-time pairing code compare (audit F12, server side)
- `auth.go` (RegisterAccount) and `api.go` (RegisterDevice) use `secureTokenEqual` instead of `!=`.

### S4.4 PBKDF2 600000 rounds + re-hash on login
- `passwordIterations` 180000 → 600000 (OWASP advice for PBKDF2-HMAC-SHA256).
- `derivePasswordKey` reuses one HMAC (Reset + Sum into the same buffer). The output is the same bytes, about 3x faster:
  - old code: 180k = 90 ms, 600k = 309 ms;
  - new code: 180k = 25 ms, 600k = 88 ms (this box).
  - So a 600k login costs about the same as a 180k login did before.
- **Every check uses the round count stored with the hash** (checked every `derivePasswordKey` / `passwordIterations` use: Login, AdminLogin, recovery, reset by code / by hash, join relay, admin bootstrap / reset, user admins). Old hashes keep working.
- **Re-hash:** after a correct user login (`LoginAccount`) or admin login (`AdminLogin`) with an old hash, a fresh salt + 600k hash is saved.
  - `rehashPassword230`: runs outside any transaction (one DB connection);
  - `WHERE id=? AND password_hash=<old>`, so a reset made at the same time wins;
  - errors are ignored (the old hash still works).
- **Not changed:**
  - hashes that phones send (join relay, reset by hash) keep the phone's count (range 1000..2,000,000, as before);
  - the phone KDF (`password_kdf.dart`, 180000) is not touched;
  - the server hash never goes to phones.

### Skipped
- S4.5 (sessionStorage): you chose to keep localStorage.
- S4.6 (`last_seen` write throttle): risk to the admin online status / presence. Not done.

## Files
- `internal/httpapi/admin_auth.go`: S4.1 + admin re-hash
- `internal/httpapi/admin_panel.go`: idle session = not active in the list
- `internal/httpapi/api.go`: S4.2 + S4.3
- `internal/httpapi/auth.go`: S4.3 + S4.4
- `internal/httpapi/bundle230_test.go` (new, 11 tests)
- `.env.example`: the new switch documented (outside the first list, docs only)

Old tests: **none changed.**

## Tests
- New: `bundle230_test.go`, 11 tests.
  - The KDF gives the same bytes as a copy of the old code, plus the RFC PBKDF2-SHA256 vector.
  - Rounds = 600000.
  - User / admin re-hash on login; a wrong password does not re-hash; a changed hash is not overwritten.
  - 181 idle days = 401; 179 = 200 and renewed; the list shows idle as not active.
  - Device-only: 403 / 401 off, works again with the switch; account devices, sign up and login unaffected.
  - Pairing compare.
- Full `go test ./...`: 5 packages ok, **286 passed (with subtests), 0 failed**. `go vet` clean.
