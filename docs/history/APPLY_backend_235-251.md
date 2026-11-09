# APPLY backend 235-251 (do this FIRST)

Zip: `locallink_backend_235-251_2026-11-10.zip` (53 files, on top of GitHub `2e89fc7` = your 230+231 push).
Holds: 235 one phone = one account, 237 past_device_ids, 236 offline lock, 238 hidden old rows (web admin), 243 HTTPS 8443, 246 tidy, 251 voice relay (off until the app uses it).
Then apply the app: `APPLY_app_3.0.73.md`.

## Commands

Before you start, run this check (from APPLY_230):
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
sqlite3 data\locallink.db "SELECT COUNT(*) FROM devices WHERE COALESCE(user_id,'')='' AND COALESCE(status,'active')='active';"
```
If it shows more than 0, start the server with `LOCALLINK_ALLOW_DEVICE_ONLY=true`.

Stop the server, then:
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_235-251_2026-11-10.zip" .
# files that moved or were deleted (a zip can't delete):
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_214.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_216.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_217.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_218.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_219.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_220.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_230.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_231.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_88b.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_88c.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_89.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_90.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_BACKEND_88.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_NOTES_49A.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_NOTES_51.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_218.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_219.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_220.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_221.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_222.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_223.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_230.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_231.md"
Remove-Item -Force -ErrorAction SilentlyContinue "test_output.txt"
Remove-Item -Force -ErrorAction SilentlyContinue "tmp_test.ps1"
Remove-Item -Force -ErrorAction SilentlyContinue "diagnose-locallink.ps1"
Remove-Item -Force -ErrorAction SilentlyContinue "generate_dev_cert.sh"
Remove-Item -Force -ErrorAction SilentlyContinue "web\admin.html"
go build ./...
go test ./...
git add -A
git add -f docs/history/*.md
git commit -m "v3 bundles 235-251 backend: one phone = one account with Waiting for proof list (235), past_device_ids in contacts (237), offline account lock + no pairing code for offline sign-ups (236), old offline rows hidden in web admin (238), HTTPS 8443 with self-signed cert proven by the server key (243), repo tidy (246), internet voice relay off by default until the app uses it (251)"
# when you are ready to push:
git push
```
Then:
- Open TCP 8443 in the Windows firewall (HTTPS). The cert is made once in `data\tls\`. HTTP 8080 keeps working.
- Start the server. Look for `TLS_AUTO_243` in its log.
- The voice relay (251) is built but nothing uses it yet. To turn it off fully: `LOCALLINK_VOICE_RELAY=off`. Max calls: `LOCALLINK_VOICE_RELAY_MAX` (default 20).

