# APPLY 231 - the server proves its key at sign in (backend + app 3.0.69+140)

Nothing has been pushed since 223, so everything comes as **two zips with one commit each**:
- `locallink_backend_230-231_2026-11-09.zip`: backend 230 + 231 (replaces the 230 zip).
- `locallink_app_224-231_2026-11-09.zip`: app 224, 225, 226, 227, 229, 231 (replaces the 224-229 zip). Its full files overwrite, so it doesn't matter if you already extracted the older zip.

**Order:**
1. **Backend first** (APPLY_230 step 1 still applies: the device-only count).
2. **Then the app.**

Both ways are safe:
- the new app works with the old server (no proof → as before);
- old apps work with the new server (no nonce → the old answer).

## PowerShell

Backend:
```
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
Expand-Archive -Force -Path "C:\Users\sajjad Ahmed\Downloads\locallink_backend_230-231_2026-11-09.zip" -DestinationPath .
go test ./...
git add -A
git commit -m "v3 bundles 230-231 backend: admin sign-in ends after 180 unused days, no-account devices off by default (LOCALLINK_ALLOW_DEVICE_ONLY), constant-time pairing code checks, PBKDF2 600000 rounds with re-hash on login (230); pairing info proves the server signing key with a nonce and an Ed25519 proof (231)"
git push
```
Then restart the server. No migration.

App:
```
cd "C:\Users\sajjad Ahmed\Downloads\LocalLink"
Expand-Archive -Force -Path "C:\Users\sajjad Ahmed\Downloads\locallink_app_224-231_2026-11-09.zip" -DestinationPath .
flutter pub get
flutter test -j 1
git add -A
git add -f APPLY_224.md APPLY_225.md APPLY_226.md APPLY_227.md APPLY_229.md APPLY_231.md TODO_224.md TODO_225.md TODO_226.md TODO_227.md TODO_229.md TODO_231.md
git commit -m "v3 3.0.69+140 bundles 224-227 + 229 + 231: faster Wi-Fi Direct/LAN connect and never use Realme/XShare groups (224), a call wakes Wi-Fi Direct with Waiting for <name> to connect (225), Bluetooth LE finds friends with Allow and Bluetooth off cards in Add Contact Nearby (226), no jump to Welcome while logged in and the call screen closes once (227), size limits on nearby socket lines so no phone nearby can fill the memory (229), the server proves its key at sign in and one pinned server key is shared with the admin trust key (231)"
git push
```
Then build the APK as usual.

## What users see
**Nothing new.** Sign in, sign up and recovery look the same. A fake server that copies the fingerprint now gets the usual red "LinkUp server identity changed. Connection was blocked for safety."

## Important
- After phones have signed in with 3.0.69 on the 231 server, **don't roll the backend back below 231**. Those phones would then say "identity changed". Fix: Reset setup on the phone.
- **Don't delete or replace `data/locallink.db`.** The server key is inside it. A new DB = a new key = every phone must Reset setup (as with the fingerprint today).
- Ongoing traffic is still plain HTTP. HTTPS is S3.

## Phone tests (after both are deployed)
1. **Phone already set up** (C71): log out, then log in → works. The log shows `PAIR_PROOF result=enforced` (or `result=pinned` if the phone never had an admin trust key).
2. **Fresh install** (ZTE): sign up or log in → works; the log shows `PAIR_PROOF result=pinned`.
3. **Admin phone:** the admin screens and blocks work. There must be no `TRUST_KEY_CHANGED` and no `old_pin_differs`.
4. **Recovery:** Forgot password → the "server online" check works.
5. **Account transfer QR:** works as before.
6. (Optional, before you deploy the backend) The new app against the old server: login works, and the log shows `PAIR_PROOF_MISSING server=old` once.

If any phone shows `PAIR_PROOF result=old_pin_differs`, send me the log. That phone saved a different server key earlier. It is not blocked, but I want to see why.

## Log lines
- `PAIR_PROOF result=pinned` (first contact) / `enforced` (old key matched, rule on): normal.
- `PAIR_PROOF result=bad|bad_signature|key_changed|missing_blocked`: refused, which is the fake-server case.
- `PAIR_PROOF result=old_pin_differs`: logged only, not blocked.
- `PAIR_PROOF_MISSING server=old`: old backend.

## Files
See `TODO_231.md` → Backend / App.

**Old tests changed:** only the version pins (`test/bundle223_logs_test.dart` 3, `test/nearby_190_test.dart` 1).

## Tests
- Backend: 5 new; full `go test ./...` (230 + 231): 5 packages ok.
- App: 17 new in `test/server_proof_231_test.dart`, plus real interop runs (the Go 231 server ↔ the Dart app code; the new app ↔ the old 223 server).
- Full Flutter suite (10 chunks): **2372 passed**. The first run had 1 failure in `bundle210a_server_test.dart` ("a 4th phone waits", connection reset on a local port). That code is not touched; the file passed 3 times alone and its whole chunk passed again (217/217). It is a timing flake. If it shows on Windows, run that file again.
