# TODO 279 - web Users shows kiran / sumaira twice ("Active" + "Not on server yet")

Status: BUILT (12 Nov "Build"): backend zip 279 + app 3.0.88+159. A-2 needed no code (the app list already uses `split238`). Was: WRITTEN. Code checked: app `de70ef7` (3.0.86), backend `106c750`.
Screenshot: `uploads/image-7.png` - rows "kiran ... Not on server yet, sajjad · Mesh, Online now" and
"sumaira ... Not on server yet, sajjad · nearby, 2 min ago", while @kiran and @sumaira are Active.

User (screenshot `uploads/image-8.png`, web Map "Server 4", Live): all 4 users ARE on the server.
So both "Not on server yet" rows are stale copies, not real waiting accounts.

## How the "Not on server yet" rows are made (verified in code)
- App (the reporter = sajjad's phone): `admin_offline_users_222.dart:150` `reportList` sends with every
  `presence_update` (`presence_service.dart:191` `offline_seen`) each saved directory card whose user id
  starts with `offline_` and whose phone was seen in the last 15 min (max 20).
  - It skips only: my own id, ids in `id_moves_218`. It does NOT skip a card when the same phone already
    has a server (non-offline) card - the 238 rule `split238` does that only for the app's own list.
- Server: `join_relay_222.go:351` `noteOfflineSeen222` upserts `seen_offline_accounts`; it drops the row
  only if the reported `device_id` is a joined device (`devices.user_id` set). It does not check
  `offline_ids` (the offline id -> real user link written at publish, `auth.go:180/:400`).
- Web `views.js:529-575` lists every row; no check against the Users list.
- The old card on sajjad's phone goes away only by an id move (`directory_repository.dart:1672`
  same phone + same signing key, or `:1730` `/api/v1/users/resolve` every 30 min) -> `local_store.dart`
  `moveUserId218` deletes the old card.

## New log evidence (12 Nov 20:11 logs)
- C71 (sajjad) moved both ids: `ID_MOVED old=457370 new=45fa14` 19:56:29 and `old=b6d2d4 new=399a00`
  19:58:01, so it stopped reporting them. The server deletes a row only when a NEW report comes for a
  joined phone, so the rows written before the publish stay forever -> B-2 (read-time filter) is the
  needed part. (The "Online now" seen later = the time of the last report, not live.)

## Why it shows now
- "Online now" / "2 min ago" = the rows are refreshed right now, so sajjad's phone still has the old
  `offline_` cards of kiran and sumaira (no id move happened), and the server did not match the
  reported phone id to a joined phone.
- Exact reason the move / match failed: NOT VERIFIED (possible: phone id in the old card differs from
  the phone id on the server, e.g. a phone that was Reset before the offline account was made; or the
  signing key changed). Not needed for the fix below.

## Fix (smallest, both sides; no schema change)
- Backend B-1: `noteOfflineSeen222` also drops + skips a report when its offline user id is in
  `offline_ids` linked to an active/suspended user (already published).
- Backend B-2: `notOnServer222` read-time filter: leave out rows whose `device_id` is a joined device
  or whose offline id is linked in `offline_ids` (old rows vanish at once, no wait).
- App A-1: `reportList` skips a card when the same phone has a non-offline directory card (reuse the
  238 rule) or `movedId218` gives a server id.
- App A-2: the app Admin -> Users "Not on server" list uses the same rule (no duplicate there either).
- Nothing else: no change to publish, join, 235 duplicate flow, or the 30-day hide.

## Tests
- Go: report for a published offline id -> no row; existing row for a joined phone or linked id ->
  not listed; a real not-yet-joined account still listed. Full Go suite (328).
- Flutter: `reportList` / pick skip both cases; a real offline account still reported. Full suite,
  analyze, release build, aapt2.

## Delivery
- Backend zip + commit (deploy first, restart the server). App part = its OWN version (user choice):
  3.0.88+159, separate zip + commit on top of 3.0.87 (278).
- User: "wait, I will add more points" - do not build until the user adds them and says "Build".
