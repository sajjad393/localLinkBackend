# APPLY backend 321 - web owner page in the 321 app style (R9, with app 3.0.112+183)

Zip: inside `locallink_321_2026-11-17.zip` (`locallink_backend_321_2026-11-17.zip`), on top of GitHub `8110af0` (backend 318).
Plan: `docs/history/PLAN_321.md` (D6, user: in 321). Push the backend FIRST, then the app.

## What changes (web owner Conversations viewer only)
- Files card on top of the chat: "1 of 3 files here" / "All files here", "2.6 MB of 9.5 MB · from ayesha's phone", one piece per file. No button (the Owner's phone brings the files).
- Photo / video tiles: coming = blurred thumbnail + ring % + "Coming by itself" + parts; next = "Next in line"; here = picture + time chip (click opens); removed = grey + "File removed after 60 days · thumbnail only".
- Voice: mic in the ring, "Coming · a / b".
- List words = the app (R1): "Messages here · x of y files coming" / "n files not available" / "Ready to read".
- Header "Ready" tag only when the messages AND all files are here.
- No new route, no chat storage on the server, no Go code change.

## Files (5 + docs)
- `internal/httpapi/adminweb/views.js`, `internal/httpapi/adminweb/styles.css`
- `internal/httpapi/admin_web_317_test.go` (listed lock change: 'Files x/y' / 'Files ✓' -> the R1 words)
- NEW `internal/httpapi/admin_web_321_test.go`, NEW `internal/httpapi/testdata/conv_321_smoke.js`
- `docs/history/PLAN_321.md`, `docs/history/TODO_321.md`, `docs/history/APPLY_backend_321.md` (git add -f)

## Tests (Linux)
- `go test ./...` all ok (cmd/server, discovery, httpapi, mdns299, store, tlsauto, ws). `go vet ./internal/httpapi/` OK. gofmt clean on the changed Go files.
- New: `TestAdminWebViewer321` (string locks + no button in the files card), `TestAdminWebSmoke321` (node, 19 checks).
- Screens (sandbox Chromium, light + dark): `web321_light.png`, `web321_dark.png`.
- NOT checked: Windows server, a real browser on the phone.

## After the push
Restart the server; reload the web page (Ctrl+F5).
