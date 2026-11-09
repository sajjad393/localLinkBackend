# Bundle 51 — Call UI freeze fix + server-relay fallback for call signaling
**Base (app):** LocalLink main @ `7d22b03` (bundle 50 already applied).
**Base (backend):** LocalLinkServer current main. **Deploy the backend FIRST.**
**Contents:**
- App zip: `lib/app/app_dependencies.dart`, `lib/features/calls/data/services/call_session_manager.dart`, `lib/features/messaging/data/services/reliable_messaging_service.dart`
- Backend zip: `internal/httpapi/websocket.go`, `internal/httpapi/websocket_schema_test.go`

Root causes are anchored in `[LocalLink] MESSAGE_FLUSH_FINISH.txt` (the dual-device capture you sent).

---

## Fix A — the 15–26 s UI freezes ("not smooth") (Dart, app)

**Evidence:** ZTE skipped 1584 / 1156 / 831 frames at a time, each inside a block of thousands of `[LocalLink] DIRECT_EVENT_RECEIVED | type=call_media_received|mesh_delivered…` lines. That capture printed the tag **11,137 times — 60% of the whole log** (~100–300/s during calls). Each line is a `developer.log` platform hop executed on the UI isolate; on the weaker handset the hop storm starved the frame budget.

**Change (`reliable_messaging_service.dart`):** hot event types (`call_media_received`, `mesh_delivered`, `mesh_queued`, `call_media_route_degraded`, `topology`, `route_invalidated`, `route_update`, `link_update`) no longer log per event. They're counted and flushed as ONE line per 10 s window:
```
[LocalLink] DIRECT_EVENTS_SUMMARY | window=10s call_media_received=412 mesh_delivered=518 call_media_route_degraded=21
```
Rare/interesting types log verbatim as before; `AppDiagnostics.record()` is untouched (in-RAM, negligible). Expected: Choreographer megaskips disappear; the call screen stays fluid on the ZTE.

## Fix B — server-relay fallback for lifecycle signaling (Dart, app)

**Evidence:** previous log had the crash at hang-up; this log shows caller X hung up (`call_end` sent over mesh) while the peer link was down → the callee never received it and kept a ghost call alive (heartbeats + reconnecting UI) for minutes.

**Change (`call_session_manager.dart`, `app_dependencies.dart`):**
1. `_sendSignal()` — mesh stays the first-class path. When `nativeMesh.sendControl` throws (PEER_UNAVAILABLE etc.) AND the signal is lifecycle-critical (`call_invite`, `call_invite_ack`, `call_accept`, `call_reject`, `call_busy`, `call_end`), the same envelope is immediately relayed over the server WebSocket (`{...msg, sender_id, transport_mode: 'offline_mesh'}`). Log marker: `CALL_SIGNAL_RELAYED … via=server reason=mesh_down`. **Heartbeats explicitly excluded** — a server ack would pretend peer-reachability that isn't true.
2. Receive side — new `_handleServerCallSignal` subscribed to `socket.messages` in `start()`: any `call_*` frame arriving over WS is normalized (marker `server: true`, `transport_mode` defaulted to `offline_mesh`) and fed through the existing `_handleLocalCallSignal` dispatcher — no duplicated business logic, all existing gates (`_signalTargetsThisSession`, state checks, `_seenInviteIds`) still apply.
3. Dual-channel dedupe — a fingerprint registry (`type|call_id|sender_id`, 10 s window) inside `_handleLocalCallSignal` drops the second copy when mesh and the server deliver the same signal (typical: massage arrives over mesh milliseconds before the relay). `call_invite` intentionally not deduped here — the 5 s route-wait resend legitimately repeats identical fingerprints and `_seenInviteIds` already guards those.
4. DI: manager now receives the existing `socket` (named parameter, nullable — test fakes unchanged).

**Honest limits (design, not bugs):** server relay won't deliver when BOTH devices' WS are also down; a relayed `call_accept` for a call the server never saw (invite went 100 % over mesh) gets a server `call not found` error — behavior unchanged from today (mesh retry path remains).

## Fix C — backend relay coverage (Go, backend) ⚠️ deploy this first

**`internal/httpapi/websocket.go`:**
1. Forwarded `call_invite` now preserves `transport_mode` + `supported_media_codecs` (previously the callee copy lost them — and the app-side local-invite gate rejects invites without them, so this is what makes Fix B's invites actually ring the peer over WS).
2. New relay case `call_invite_ack` — callee→caller, validated (callee only, status `ringing`), rate-limit shared with signaling. Lets the ring-back / "ringing" state reach the caller even when mesh is down.
3. New relay case `call_peer_video_state` — mirrors `call_accept` shape; carries a new `enabled` bool + `selected_media_codec` (`WSMessage` gained `SupportedMediaCodecs []string` and `Enabled *bool`; both `omitempty`, zero wire impact on existing flows). Covers the video-upgrade signal for app-side fallback once the app emits it over WS — **today the app's video toggle still goes out over mesh (native layer)**, so this case is a safe facility for the next iteration, not a behavior change.
4. New `websocket_schema_test.go` — pins the wire contract the app relies on (`transport_mode`, `supported_media_codecs`, `enabled` key-name/veracity round-trip).

## Gates
- App: `flutter analyze` **0 errors** (214 issues = pre-existing infos/warnings only; no new diagnostics in the three edited files); `flutter test` call + transport + route-policy + message suites: **14/14 passing**.
- Backend: `gofmt` clean, `go vet`/`go test` **could not run in my sandbox** (module-cache/proxy corruption on linux-amd64 — deterministic, unrelated to this patch; your Windows PC ran `go test` fine before). **Please run `go vet ./... && go test ./...` locally before deploying.** via the call lifecycle.

## Operation order & evidence to watch
1. `Extract backend zip → LocalLinkServer`, `go vet ./... && go test ./...`, redeploy server.
2. Extract app zip → LocalLink, review diffs (no Kotlin this time!), `flutter build apk --debug`, install on both phones.
3. During a call you should see `DIRECT_EVENTS_SUMMARY` lines every ~10 s instead of the firehose, and (only when the direct link is actually down) `CALL_SIGNAL_RELAYED … via=server` — after which the peer's UI must immediately show the call/ring/end.

**Suggested commit messages**
- App: `fix calls/media: sample hot direct-event logs (freeze fix); server-WS fallback for lifecycle call signaling with dual-channel dedupe`
- Backend: `feat ws relay: preserve transport_mode + media codecs in forwarded invites; relay call_invite_ack and call_peer_video_state between participants`
