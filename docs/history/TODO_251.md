# TODO 251 - Online voice calls through the server (Phase 15, P9)

## Status
- Server part: BUILT (backend zip). Off for old apps (they never send it). Safe to deploy.
- App part: PLAN + MOCK only (needs native Kotlin, see below). Mock: `mocks/mock_251_internet_call.jpg`.

## Server (built) - `internal/httpapi/voice_relay_251.go`
- New WebSocket message `call_voice` {call_id, voice=<base64 locked voice piece>}.
- Passed to the other phone only when the call is `connected` and the sender is one of its two phones
  (checked in the DB every 5 s, cached in memory between - no DB hit per piece).
- The server never opens it: it is already locked with the per-call key (bundle 159, AES-256-GCM).
- Limits: per phone 1 relayed call, 80 pieces/s, 1400 base64 chars a piece (nonce+tag+voice >= 29 bytes);
  server 20 calls at once (`LOCALLINK_VOICE_RELAY_MAX`), `LOCALLINK_VOICE_RELAY=off` turns it off.
- A slow phone loses pieces (new `Hub.TrySendTo`), its socket is never closed for voice.
- Refused -> one `call_voice_stop` {reason: not_in_call | call_over | server_full | one_call_only | relay_off} per 10 s.
- Logs: `VOICE_RELAY_251 at=start|run|end call=<6> passed= dropped=`.
- Tests: `voice_relay_251_test.go` (4), `internal/ws/hub_251_test.go` (1). Full Go suite passes.
- Bandwidth: one call = about 2 x 50 pieces/s x ~150 B = ~15 KB/s each way through the PC.

## App (to build in a native cycle)
1. Kotlin `CallMediaSession`: when `CALL_PATH path=none` (no WD/LAN/mesh) and the server is connected,
   hand each sealed frame to Dart through the existing event channel -> Dart WebSocket `call_voice`.
   Incoming `call_voice` -> method channel -> the same `handleIncomingSealedVoice` path (no new crypto).
2. `startCall`: today it needs a nearby route; allow "server" when no route and the server is connected
   and the server is new enough (pairing info has `voice_relay:1` - add when the app part is built).
3. Nearby first: every 2 s path check (bundle 192) - when WD/LAN/mesh comes back, switch back and stop the relay.
4. UI: chip "Calling over internet" under the timer + "Locked end to end. The server only passes it on." (mock).
   No camera button (248 hides it: not a nearby call).
5. Jitter: server path has 50-300 ms delay -> start the jitter buffer at 120 ms on this path.

## What you test (after the app build)
- C71 on mobile data / other Wi-Fi with the server reachable, Note 60x at home: 3 min call, then walk into range -> chip turns "Wi-Fi Direct call".
