# APPLY 217 – backend (web panel only: 217a, 217b, 217e)

No database change, no new route, no Go code change except one new test.
Built on top of backend 216 (`3a0fa5e`). **Deploy this first, then install the 217 app.**

## What changed

**217a – Web panel fully responsive**
- Sizes: phone up to 640 px, tablet 641–1000 px, PC above 1000 px. The page never scrolls sideways.
- Phone top bar on one line: menu, page title, refresh, bell, crowned photo. Name, level, light / dark and Sign out are in the photo menu (new head with your photo).
- "Sign out" never wraps to 2 lines.
- Drawer (up to 1000 px): 280 px wide, X button in its head, swipe left closes it.
  - Fixed: on a phone the menu button opened and closed the drawer in the same tap (a page-wide click handler shut it). The menu button and the drawer no longer close it.
- Tables become cards on a phone (Users, Devices, Sessions, Calls, Files, Recovery, App version …): name on top, status pill top-right, the rest as small labelled values, buttons bottom-right.
- Tablet: Devices hides the Build column; row buttons wrap to 2 lines.
- Manage person on a phone: full screen with a sticky head (back arrow + "Manage {name}" + X), full-width buttons. Dialogs open as bottom sheets.
- Chip bars scroll sideways inside themselves; chart shows 4 date labels on a phone.

**217b – Map page**
- Aerial (flight-path) curved lines that bow away from the middle, coloured by link (server green, Wi-Fi Direct amber, mesh purple, local Wi-Fi teal).
- Each line has a soft glow, moving dashes (one loop every 2 s) and a glowing dot travelling along it (one trip every 3 s, not in step).
- Blocked = grey dashed line, never moves.
- Motion stops with "reduce motion" on the phone / PC, and while the tab is hidden.
- Opens fitted to everyone; new "Fit everyone" button (4 corners icon); pinch zoom with two fingers; drag with one.
- Fixed: the server icon is centred in its circle again.
- Light theme: light map with darker dashes.
- New crown (gradient body, 3 pearls, red + blue jewels, shine), 1.1 × photo radius, sitting on the gold ring.

**217e – Test**
- New `internal/httpapi/admin_web_217_test.go`. Old tests unchanged.

## Files
- `internal/httpapi/adminweb/styles.css`, `index.html`, `app.js`, `components.js`, `views.js`
- new `internal/httpapi/admin_web_217_test.go`
- `APPLY_217.md`

## Checked here
- `go vet` + `go test ./...` green.
- The panel loaded in a headless DOM (jsdom) at 390 px and 800 px: Map (3 lines, 1 blocked, 2 moving dots, fit button), Users / Devices / App version as cards, Manage head, photo menu, drawer open + X close – no script errors. Real browsers were not available here, so please do the PC / phone tests 217-1 … 217-7 in `TODO_217.md`.

## PowerShell (backend, first)
```
cd <LocalLinkBackend folder>
git pull
Expand-Archive -Force "$HOME\Downloads\locallink_backend_217_2026-11-05.zip" .
git add -A
git add -f APPLY_217.md
git commit -m "v3 bundle 217 backend: web panel fully responsive (phone cards, compact top bar, drawer close), aerial moving map lines with fit and pinch, new crown"
if ($LASTEXITCODE -eq 0) { git push }
```
Then deploy the server as usual (no migration). Hard refresh the panel once (Ctrl+F5) so the browser loads the new files.
