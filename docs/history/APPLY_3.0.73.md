# APPLY all unpushed: app 3.0.73+144 + backend 235-251

ONE zip with all unpushed code: `locallink_all_unpushed_2026-11-10.zip`.
- `LocalLink/` = app 3.0.73+144 (on top of GitHub `e5f7494`; holds 235 + all phases).
- `localLinkBackend/` = backend 235-251 (on top of GitHub `2e89fc7`).
- One commit per repo. Deploy the BACKEND first. Replaces the old 235 zips (do NOT apply those too).

## 0. Unpack once
```powershell
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_all_unpushed_2026-11-10.zip" "$env:USERPROFILE\Downloads\locallink_all"
```

## 1. Backend (first)

Before you start, run this check (from APPLY_230):
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
sqlite3 data\locallink.db "SELECT COUNT(*) FROM devices WHERE COALESCE(user_id,'')='' AND COALESCE(status,'active')='active';"
```
If it shows more than 0, start the server with `LOCALLINK_ALLOW_DEVICE_ONLY=true`.

Stop the server, then:
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
Copy-Item -Recurse -Force "$env:USERPROFILE\Downloads\locallink_all\localLinkBackend\*" .
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
```
Then:
- Open TCP 8443 in the Windows firewall (HTTPS). The cert is made once in `data\tls\`. HTTP 8080 keeps working.
- Start the server. Look for `TLS_AUTO_243` in its log.
- The voice relay (251) is built but nothing uses it yet. To turn it off fully: `LOCALLINK_VOICE_RELAY=off`. Max calls: `LOCALLINK_VOICE_RELAY_MAX` (default 20).

## 2. App
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\LocalLink"
Copy-Item -Recurse -Force "$env:USERPROFILE\Downloads\locallink_all\LocalLink\*" .
# old doc places (moved to docs\history):
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_199.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_199b.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_200.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_201.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_202.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_203.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_204.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_205.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_206.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_207.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_208.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_209.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_210.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_211.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_212.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_213.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_214.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_215.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_216.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_217.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_218.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_220.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_221.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_222.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_223.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_224.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_225.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_226.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_227.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_229.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_231.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_232.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_233.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_G7.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_G7_204.md"
Remove-Item -Force -ErrorAction SilentlyContinue "APPLY_groups.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_200.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_201.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_202.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_203.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_204.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_205.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_206.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_207.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_208.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_209.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_210.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_211.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_212.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_213.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_214.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_215.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_216.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_217.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_218.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_220.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_221.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_222.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_223.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_224.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_225.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_226.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_227.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_229.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_231.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_232.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_233.md"
Remove-Item -Force -ErrorAction SilentlyContinue "TODO_G7.md"
flutter pub get
flutter test
git add -A
git add -f docs/history/*.md
git commit -m "v3 3.0.73+144 bundles 235-254: one phone = one account (235), one chat per person on a new phone (237), offline account goes up by itself (236), block never dropped + Show hidden (238), server-connected colour (239), notification letter + My QR (242), HTTPS with pinned cert (243), security code (245), repo tidy + key warning (246), video logs (247), video button in nearby calls (248), admin actions fixes: wait for trust, replace old card, same-phone refuse, empty chat, live status, You avatar, pinned bar, Tap to cancel (254)"
```
Back up `C:\Users\sajjad Ahmed\.android\debug.keystore` (USB + cloud). A release build now prints a WARNING that it uses the debug key. That's expected.

## 3. Phone tests (short)
1. 235: make a new account on a phone that friends know. It must show "Confirm your account". Recovery code works offline; the old password works with the server. Web admin "New owner" frees the phone.
2. 237: a friend moves to a new phone. You should see ONE chat with old + new messages, and the custom name + private mark kept.
3. 236: an account made offline goes up by itself once the server is reached (no password or code asked).
4. 238: block someone while offline. It reaches the phone later (tries every 30 min and on link back). Old offline admin rows are behind "Show hidden (N)".
5. 243: after about 20 s on the server Wi-Fi the log shows `TLS_243 result=on url=https://...:8443`.
6. 245: Chat > Conversation info > Security code. Both phones show the same 60 digits; scanning matches.
7. 248: camera button in a connected nearby call (WD/LAN). First tap may only ask permission. Test on the ZTE too.
8. 254 (admin):
   - A Block for noshad shows "reached the phone · it must reach the server once" until his phone reaches the server.
   - A new Block/Unblock for the same person shows the old one as "Replaced".
   - A chat request between two accounts on the same phone is refused.
   - A chat with no messages ends as "No messages".
   - Admin status/way matches the chat header.
   - The map "You" shows your own letter/photo.
   - The "Link paths" L is no longer cut.
   - The orange bar stays under the title.
   - Pending rows show "Tap to cancel".

## 4. Not done (needs you)
- Logs: Phase 5 acks (both phones together for 1 h after noshad re-adds sajjad), Phase 6 voice gaps, 247 a video that stops early (+ time), 21.10 read ticks (both phones 9 Nov 21:09-23:15).
- P0 audit steps (no code), 228 idea, ZTE day test.
- HTTPS part C and HTTP off: only after all phones run HTTPS. Say go when ready.
- 250 group voice and the 251 app side need native Kotlin + a phone build cycle (plans: TODO_250.md, TODO_251.md).
- 249: mesh voice already works. Hop cap kept at 8 (my advice); say if you want 2.
- Phase 20 structure cleanup skipped (stability).

## 5. Old tests changed (setup only)
- `test/support/b220_fakes.dart`: `Phone220.trust` is mutable; `Did220.chat` field (default 'refused:unknown', same as before).
- Version pins: `test/bundle223_logs_test.dart`, `test/nearby_190_test.dart`.
