# TODO 274 - Real-time admin map over a socket (web + app admin map)

Status: PROPOSAL ONLY (freeze ON). Build only after the user says "Build".
User request (14 Nov): "update data from socket on map. show realtime data".
Code: backend `86aa525`, app `6bf30bd`.

## Today (code)
- Web Map / Network pages load `GET /api/v1/admin/link-map` ONCE when opened. The auto-refresh
  (`adminweb/app.js:1278-1290`) only refreshes Dashboard, Actions and Conversations, NOT Map / Network.
  So the map is a snapshot until you click again.
- "Online" in the link map = `last_seen_at` < 2 min (`admin_216.go:548,567`), so a phone that left
  still shows online for up to 2 min - although the server knows at once (socket close ->
  `setPresence(deviceID,false)`, `websocket.go:114-120`).
- Phone-to-phone lines come from phone reports (`presence_update` with `neighbors`,
  `websocket.go:162`, `noteNeighbors216` `admin_216.go:459`). Phones report every 30 s and AT ONCE on a new
  link (`presence_service.dart:73-76`, `peer_connected` -> publish force), but NOT when a link drops.
- The device socket `/ws` refuses tokens in the URL (`websocket.go:80`); the server has its own WS code
  (`internal/ws`), so no new dependency is needed.

## Plan
### Server (backend zip)
R-1 New admin live socket `GET /api/v1/admin/live` (plain `ws://`, keep http). The browser cannot send an
    Authorization header on a WebSocket, so the FIRST frame must be `{"type":"auth","token":"<admin token>"}`
    within 5 s; checked with the same admin rule as `GET /admin/link-map` (read permission). No token in
    the URL (same rule as `/ws`). Max 5 live sockets per admin; ping every 25 s.
R-2 The server sends `{"type":"link_map", nodes, edges, at}` (same JSON as `GET /admin/link-map`) right
    after auth and again whenever something changes, at most once per second (changes are collected):
    - a phone's socket connects / disconnects (hub add / remove);
    - a phone's neighbour list changes (`noteNeighbors216` compares with the old list);
    - block / unblock / remove phone by an admin.
R-3 "Online" = connected to the server NOW (the live presence `ServerConnected`), falling back to
    `last_seen_at` < 2 min only when there is no live presence entry. A phone that leaves turns offline
    at once instead of after 2 min.
R-4 Phones whose account is an active admin get a tiny `admin_link_map_changed` frame on their normal
    device socket (same 1 s collecting); nothing for normal users.

### Web (backend zip, adminweb)
W-1 Map and Network pages open the live socket and redraw on every `link_map` frame. Reconnect after
    1, 2, 5, 10 s; while the socket is down, fall back to `GET /admin/link-map` every 15 s.
    Small "Live" dot (green) / "Reconnecting..." (grey) next to the page title.
W-2 A redraw keeps your zoom / pan and keeps phones where they are (new phones get a free spot); only
    lines change. Socket closed when you leave the page or the tab is hidden; reopened when it is back.

### App (3.0.85)
A-1 `presence_service.dart`: a dropped link (`peer_disconnected`) also publishes at once (today only a new
    link does), so a lost Wi-Fi / WD link reaches the map in ~5 s instead of up to 30 s.
A-2 Admin map (TODO_272 B-1): on `admin_link_map_changed` fetch `GET /admin/link-map` at once; the
    15 s poll becomes a 60 s safety poll.
A-3 The user "My links" card (TODO_273) is already real time (PresenceHub, local); no change.

## How fast it will be (honest)
- Phone joins / leaves the server: about 1 s.
- New phone-to-phone link: about 1-5 s (phone publishes at once; 5 s floor between two publishes).
- Lost phone-to-phone link: about 5 s after the phone itself notices it (noticing can take longer when a
  link fades instead of closing).

## Tests
- Go: no auth frame -> closed; wrong token -> closed; good token -> `link_map` frame; disconnect ->
  offline in the next frame; 10 changes in 1 s -> 1 frame; device without admin -> no hint frame.
- Web: static test like `admin_web_216_test.go` (live socket used on Map / Network, no token in URL).
- Dart: `peer_disconnected` triggers publish (force); admin hint triggers one link-map fetch.

## Files if built
- Backend: `cmd/server/main.go` (route), new `internal/httpapi/admin_live_274.go`, `admin_216.go`
  (online rule + change hook), `websocket.go` (hooks on connect / disconnect / presence), `adminweb/app.js`,
  `adminweb/views.js`, new tests `admin_live_274_test.go`, `admin_web_274_test.go`.
- App: `presence_service.dart`, `admin_panel_screen.dart`, the socket message handler for the hint,
  new `test/bundle274_live_map_test.dart`.

---
# Part D - Dashboard "Live network" card (web screenshot 14 Nov) + app dashboard

## D-1 (bug) Web: every colour is the same blue
- `adminweb/views.js:249-255` gives each segment `var(--success)`, `var(--primary)`, ... and turns them into
  colours with `cssVarToHex` (`views.js:395-401`). That cuts `var(--success)` to `--success` and calls
  `ui.token('--success')`, and `token` (`components.js:130-133`) adds `--` AGAIN -> `----success` -> not found
  -> the fallback `#1e5bff` (blue) for every slice and every legend dot. Root cause confirmed in code.
- Fix: strip the leading `--` in `cssVarToHex`, and use the same link colours as the maps: Server blue,
  Local Wi-Fi teal, Wi-Fi Direct orange, Mesh purple, Offline grey.

## D-2 (wrong picture) The donut adds numbers that overlap
- Slices = Server connected 3 + LAN available 1 + Wi-Fi Direct 1 + Mesh capable 1 + Offline 1 = 7 slices
  for 4 phones: one phone can be in several groups, so the ring is not "parts of a whole".
- "Mesh capable" / "LAN available" are engine flags from the phones (`presence_service.dart:113-115`:
  `lan_active`, `transport_active` = "engine on"), not links in use (same root as TODO_272 N-1).
- Fix (web + app): the ring = each phone ONCE by its way now (from the link map: Server / Local Wi-Fi /
  Wi-Fi Direct / Mesh / Offline), so slices add up to the phones. The row under it shows LINKS from the
  link map: "Server 3 · Wi-Fi links 3 · Wi-Fi Direct 0 · Mesh 0 · Phones 3". "Mesh capable" removed.
- "Registered 4" counts every device row incl. removed ones (`admin_panel.go:459`
  `SELECT COUNT(*) FROM devices`), while the map shows 3 -> web shows "Phones" from the link map
  (active, not removed); the API number stays for other pages.

## D-3 App dashboard (same card)
- `admin_dashboard_screen.dart:700-715` builds the same 5 overlapping slices from `networkData`.
  Colours there come from the app palette (not the web bug), but primary / info are both blue.
- Fix: same rule as D-2 and the same link colours (from `kLinkTagColors`).

## D-4 Real time on the dashboards
- Web: the Live network card listens to the live socket (R-1/R-2) instead of the 30 s+ poll
  (`app.js:1253`, floor 30 s); the rest of the dashboard keeps its poll.
- App: the admin dashboard and Network screen refresh their server data on `admin_link_map_changed`
  (R-4) instead of only when the panel opens / the server comes back (`admin_panel_screen.dart:149,166`).

Files (extra): `adminweb/views.js` (cssVarToHex + card), `admin_dashboard_screen.dart`,
`admin_network_screen.dart`, tests in the same new test files.
