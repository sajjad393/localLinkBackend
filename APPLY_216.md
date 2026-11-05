# APPLY 216 – backend (216h + 216i)

Backend for app 3.0.57+128 · **no migration** (still v57) · additive JSON fields only, so old apps keep working.
Zip: `locallink_backend_216_2026-11-05.zip`. **Deploy the backend first, then install the app.**

## What changes

**216h – data for the new admin pages (app + web)**
- New `GET /api/v1/admin/link-map` (any admin level). It returns one node per phone (name, photo yes/no, admin level, blocked, online, link) and the lines between phones.
  - Lines come from the `neighbors` list that phones (216g) send in their `presence_update`.
  - The server keeps only the newest report of each phone, in memory. A report older than 4 minutes is dropped. At most 32 neighbours are kept per phone. Unknown phones get no line.
  - If both phones report each other, the map draws one line, not two.
- New `GET /api/v1/admin/users/{id}/avatar` (any admin level). It sends the profile photo, or 404 when the person has none. Normal user tokens are refused.
- Extra fields, all `omitempty` (names fall back to the username):
  - Users: `display_name`, `has_avatar`, `last_seen_at`
  - Devices: `display_name`, `has_avatar`, `app_build`
  - Sessions: `display_name`
  - Calls (admin list only): `caller_user_id`, `caller_name`, `callee_user_id`, `callee_name`
  - Security events: `username`, `display_name`, `device_name`
  - Audit log: `admin_name`, `target_name` (an "idA|idB" target becomes "A ↔ B")
  - File transfers: `owner_user_id`, `owner_name`, `kind` (photo / video / voice / document). **The file name is never sent.**
  - Recovery requests: `user_id`, `display_name`, `has_avatar`
  - Admin settings: `last_backup_at`, `last_backup_bytes` (newest `.db` in `LOCALLINK_BACKUP_DIR`), `max_file_bytes`, `device_quota_bytes`, `tls_enabled` (on when both `LOCALLINK_TLS_CERT` and `LOCALLINK_TLS_KEY` are set)
- Roboto fonts (`Roboto-Regular/Medium/SemiBold/Bold.ttf` + `OFL.txt`) are now built into the server under `/admin/fonts/`. No CDN is used, so the panel still works with no internet.

**216i – web panel redesign (mock v4 W1–W15, UI_SPEC_216 §4)**
- Dark by default, with the Roboto216 font, the spec colours, and the sizes for cards, tables, pills, chips and buttons. Light mode is still in the name menu.
- Sidebar groups are People / Network / Safety / System. Map has a "New" tag. "Owner" shows next to Conversations, App version and Admin settings.
- The header shows the photo with the crown, the name, the level and a **Sign out** button.
- **Map (new):** chips with counts, drag and wheel zoom, + / − / centre buttons, line colour = link type, blocked = grey photo with a red ring and a dashed line, and the "Phones (N)" card. Click a person to see only their phones.
- **Users:** search plus the chips All / Admins / Active / Suspended / Off. Each row has the photo, crown, level, phones, status and last seen. One **Manage** button opens the side panel, which has:
  - Status cards
  - Admin level cards
  - Wi-Fi rule chips (Default / Allowed / Always on / Always off)
  - See phones
  - Sign out all phones
- **Devices:** chips All / Online / Removed and the columns Phone / Owner / Link now / Last seen / Build. The Wi-Fi rule uses the same 4 words. (Fixed: the old prompt sent "allow" / "block", which the server refused.)
- **Sessions:** two tables, "People signed in" and "Admins signed in" ("This one" marks your own sign-in).
- **Network & mesh:** 4 tiles and a "Phones right now" table that includes "Linked with".
- **Calls / File transfers:** names and photos; Done / Missed / Live; the kind of file and who sent it.
- **Recovery:** each waiting request is a card with a reason box for Reject, plus an Earlier table.
- **Security:** Serious / Small / Locked and "This week", written in plain words (wrong passwords are grouped per person per day).
- **Audit log:** chips All / Blocks / Admins / Recovery and a timeline split into Today / Yesterday / Earlier, written as sentences ("admin blocked Ali").
- **Dashboard:** 4 tiles, a Map card ("Open map ›") and "Needs a look". The charts and health below are unchanged.
- **Admin settings:** plain words, a "Last: …" line on the backup card, and a Server limits card.

## Files
New:
- `internal/httpapi/admin_216.go`
- `internal/httpapi/admin_216_fields_test.go`
- `internal/httpapi/link_map_test.go`
- `internal/httpapi/admin_web_216_test.go`
- `internal/httpapi/adminweb/fonts/Roboto-Regular.ttf`, `Roboto-Medium.ttf`, `Roboto-SemiBold.ttf`, `Roboto-Bold.ttf`, `OFL.txt`

Changed:
- `cmd/server/main.go` (2 routes)
- `internal/httpapi/admin.go`, `admin_panel.go`, `calls.go`, `recovery.go`, `websocket.go`, `admin_web.go`
- `internal/httpapi/adminweb/index.html`, `styles.css`, `components.js`, `views.js`, `app.js`

No old test was changed. `go test ./...` passes (all packages). The web panel was also loaded against a real local server (jsdom): every page rendered with no script errors.

## Checks after deploy
- [ ] 216h-1 Open `/admin`: dark panel with the Roboto font. The sidebar shows People / Network / Safety / System, and Map has "New".
- [ ] 216h-2 Users: photos and crowns show. Manage → change Ali to Suspended → his photo turns grey with a red ring.
- [ ] 216h-3 Map: with both phones on the same Wi-Fi Direct group (app 216g installed), a line appears between Sajjad and Sumaira within about 1 minute. It goes away about 4 minutes after they split.
- [ ] 216h-4 File transfers shows "Photo · Sajjad" and never a file name.

## PowerShell (backend, first)
```
cd <LocalLinkBackend folder>
git pull
Expand-Archive -Force "$HOME\Downloads\locallink_backend_216_2026-11-05.zip" .
git add -A
git add -f APPLY_216.md
git commit -m "v3 bundle 216 backend: admin link map from phone neighbours, names and photos on admin lists, admin avatar route, Roboto fonts, web panel redesign with Map page"
if ($LASTEXITCODE -eq 0) { git push }
```
Then deploy the server as usual (no migration).
