# TODO 231 - audit S2: the server proves its key (backend + app 3.0.69+140)

Go ahead: 2026-11-09 ("Go ahead", after 230).
Base: backend 230 (on `1f995eb`); app 229 (3.0.68+139, on `4f486df`).

## Your choice (ask_user 2026-11-09)
- A brand-new phone trusts the server **automatically, like today. No new screen.** So there is no mock and no UI change.

## Why (audit F3, F4)
- F3: the "pin" was a compare of the public fingerprint. Any machine can copy it and pass.
- F4: the admin trust key was saved from the first `/api/v1/trust` answer, and a different key later was only logged.

## Goal
Only a machine that holds the real server **signing key** passes sign in, sign up or recovery. The phone keeps **one** server key: the pairing key and the admin trust key are the same setting.

## Backend (deploy first, backward compatible)
- `GET /api/v1/pairing/info?nonce=<base64url, 16..64 bytes>` adds:
  - `server_key`: the Ed25519 public key, the same as `/api/v1/trust`;
  - `key_id`, `ts`;
  - `proof` = signature over `ll-pair-v1|<server_id>|<fingerprint>|<nonce>|<ts>`.
- Without a nonce the answer is exactly the old 6 fields, so old apps are unaffected.
- A bad nonce (wrong size, or a character that is not base64url) → 400 `BAD_NONCE`.
- Every other server signature is over JSON (`{...`), so a pairing proof can never be taken for one of them.
- The signing key is **not** in any leaked DB copy. I checked every version of `data/*.db` in the repo history: the `admin_settings` table has no signing key row. So the proof can't be faked with the leaked data.
- Files:
  - `internal/httpapi/api.go`: `PairingInfo`;
  - `internal/httpapi/pairing_proof_231.go` (new);
  - `internal/httpapi/pairing_proof_231_test.go` (new, 5 tests).

## App (3.0.69+140)
- `lib/core/security/server_proof_231.dart` (new):
  - nonce;
  - `verifyAnswer`, which checks the Ed25519 signature;
  - the `check` / `pin` rule.
- `locallink_api.dart`:
  - `pairingInfo()` and `probePairingInfo()` both go through `_pairingInfoFrom`;
  - a fresh nonce is sent every time;
  - a proof that doesn't verify throws "identity changed". This covers sign in, recovery and account transfer.
- `authentication_models.dart`: `PairingInfo` gets optional `serverKey`, `keyId`, `proofTs`, `proof` and `proofOk` (old constructors unchanged).
- `authentication_repository.dart` (`prepareForAuthentication`) and `recovery_repository.dart` (`prepareForRecovery`, `serverOnline222`):
  - `ServerProof231.check` runs before trusting;
  - `ServerProof231.pin` runs after.
- **The rule:**
  - **Key setting** = `server_trust_key`, the same setting as `AdminBlockService.trustKeySetting`.
  - **First contact:** a verified proof pins the key and turns the rule on (`server_proof_seen_231=1`).
  - **Key saved before 231 and equal** to the proof key → the rule turns on.
  - **Rule on** → a different key, or an answer **without** a proof, = "LinkUp server identity changed. Connection was blocked for safety." (the same text and path as today's fingerprint change).
  - **Key saved before 231 that differs** → only logged (`PAIR_PROOF result=old_pin_differs`). It is never blocked and never replaced, so no phone that works today gets blocked by this bundle.
  - **Old server (no proof) and rule not on** → works as before; `PAIR_PROOF_MISSING` is logged once.
- `admin_block_service.dart` `pinTrust()`: reads the setting again before deciding.
  - Bug fixed: `load()` caches the key once. If pairing pinned the key after `load()`, `pinTrust` could overwrite it with whatever `/api/v1/trust` answered.
  - A test fails without the fix.
- Version 3.0.69+140. The version pins are in `bundle223_logs_test.dart` (3) and `nearby_190_test.dart` (1).
- Reset setup empties the settings table, so it clears the pinned key and the rule (as before).

## Not in 231
- Ongoing traffic is still plain HTTP. A machine in the middle that passes the real proof along could still read or change later requests. That is S3 (HTTPS with a pinned certificate whose hash the proof will cover).
- Account transfer: the new phone gets the key at its first sign-in check or admin sync (as today). The QR doesn't carry the key.

## Log lines
- `PAIR_PROOF result=pinned|enforced|bad|bad_signature|key_changed|missing_blocked|old_pin_differs`
- `PAIR_PROOF_MISSING server=old` (once per app run)

## Tests
- Backend: 5 new tests; full `go test ./...` with 230 + 231 = 5 packages ok.
- App: `test/server_proof_231_test.dart`, 17 tests:
  - the proof: replay, tampered parts, another key, garbage;
  - the pin rule (6 cases);
  - the API end to end against a local test server (good, replayed, forged, old server);
  - wiring order;
  - `pinTrust` never overwrites, and still saves on an empty phone.
- **Real interop (done here):**
  - the real Go 231 server's proof verifies in the Dart app code, and its key equals `/api/v1/trust`;
  - the new app against the real old 223 server: pairing works (no proof).
- Old tests: only the version pins.
- Full Flutter suite: 2372 passed (one timing flake in `bundle210a_server_test.dart` passed on re-run; see APPLY_231).
