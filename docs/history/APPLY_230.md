# APPLY 230 - backend: admin sign-in 180 unused days, no-account devices off, 600k password rounds (backend only)

**Order:** deploy this after 224-229 are pushed, or before. It does not depend on them and the app does not change.

## 1. Before you deploy: check for old device-only rows (1 minute)
The LinkUp app always uses an account, so this should be 0. If you have `sqlite3`, run this on the server DB (the server can keep running):
```
sqlite3 data\locallink.db "SELECT COUNT(*) FROM devices WHERE COALESCE(user_id,'')='' AND COALESCE(status,'active')='active';"
```
- **0** → nothing to do.
- **More than 0** → those are old test / pre-account clients. They will stop working. To keep them, start the server with `LOCALLINK_ALLOW_DEVICE_ONLY=true` (PowerShell: `$env:LOCALLINK_ALLOW_DEVICE_ONLY="true"` before starting it).
- No sqlite3 → just deploy. If `DEVICE_ONLY_REFUSED` lines appear in the server log, look at the device id and decide.

## 2. PowerShell
```
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
Expand-Archive -Force -Path "C:\Users\sajjad Ahmed\Downloads\locallink_backend_230_2026-11-09.zip" -DestinationPath .
go test ./...
git add -A
git commit -m "v3 bundle 230 backend: admin sign-in ends after 180 unused days, no-account devices off by default (LOCALLINK_ALLOW_DEVICE_ONLY), constant-time pairing code checks, PBKDF2 600000 rounds with re-hash on login and a 3x faster KDF"
git push
```
Then restart the server as usual. No migration.

## 3. What changes for people
- **Users:** nothing they can see. On the next correct login, the server quietly upgrades the stored password hash (180k → 600k rounds). Login time stays about the same, because the KDF code is now 3x faster.
- **Admin (web console and admin phone):** stays signed in as long as it is used at least once every 180 days. After 180 days without use it must sign in again. The Sessions list shows such a session as not active.
- **Old device-only clients (no account):** refused (403 / 401) unless `LOCALLINK_ALLOW_DEVICE_ONLY=true`.

## 4. Quick checks after deploy
1. Phone A logs out and logs in again → works (this also upgrades the hash).
2. Admin web console: refresh → still signed in. Sign out and in → works.
3. Admin phone: open the admin screens → work as before.
4. Send a message A → B through the server → arrives.
5. Server log: no `DEVICE_ONLY_REFUSED` (or only for devices you know are old).

## Log lines
- `DEVICE_ONLY_REFUSED device=<8 chars>`: at most one a minute; an old no-account token was refused.

## Tests
- New `bundle230_test.go`: 11 tests. Old tests: none changed.
- Full `go test ./...`: 5 packages ok, 286 passed, 0 failed. `go vet` clean.

Details: `TODO_230.md`.
