# TODO 272 - Admin map: app shows the whole network like the web (option B) + map readability

Status: PROPOSAL ONLY (freeze ON). Build only after the user says "Build".
User decision (14 Nov): "option B - this map is only for admin in both mobile and web".
Scope chosen: B-1..B-5 + A-1/M-1 blue server lines (app + web) + M-2 + M-3 + A-3. M-4 = KEEP (no chip change).
Code: app `LocalLink` @ `6bf30bd`, backend `localLinkBackend` @ `86aa525`.
Evidence: web screenshot (6 lines) + app screenshot 12 Nov 14:48 (3 lines: You-Server, You-noshad, You-Ibrahim).

## Why they differ today (code)
- Web: `GET /api/v1/admin/link-map` (`admin_216.go:511-620`) = every active device, `online` = seen < 2 min,
  edge server->phone for each online phone, phone<->phone edges from the phones' neighbour reports
  (TTL 4 min; phones report every 30 s, `presence_service.dart:77`).
- App: `admin_panel_screen.dart:351-399` builds nodes from this phone's own view, ONE tag per phone
  (`_tagFor`: hub way > WD > mesh > lan > server); the painter (`link_paths_map.dart:845-900`) draws only
  You->Server, server->phones tagged server, and You->phone. No phone<->phone lines.
- The app ALREADY downloads the same server map once: `AdminBloc.load()` -> `linkMap()`
  (`admin_bloc.dart:185-217`, `state.linkMapData`; used by the Devices/Network rows via `edgeOf216`).
  It is loaded only when the panel opens / the server comes back, so it is not live, and the map ignores it.

## Part B - app (option B)
B-1 Live server map while the admin panel is open
- `admin_panel_screen.dart`: when the server is connected, fetch `linkMap()` on open and every 15 s
  (only this one call, not the whole `load()`); keep the answer + time. Ignore it when the server is
  off or the answer is older than 60 s. Uses the existing admin session/token; 401 -> existing
  `_onBlocState` renewal path is not touched (link-map errors are just ignored).
B-2 Merge (new pure function, new file `lib/features/admin/presentation/widgets/link_map_network_272.dart`)
- Nodes = this phone's own nodes (unchanged rules) + server nodes that are online and missing locally
  (tag server). Own device ids never show as another phone; removed/blocked rules as today.
- Extra lines from the server map:
  - server -> phone for every online phone that is not already drawn (also for phones tagged Wi-Fi /
    WD / mesh: this is the missing noshad/Ibrahim server line);
  - phone <-> phone lines where NEITHER end is this phone (noshad <-> Ibrahim).
  - Lines that touch this phone keep coming from this phone's own live view (instant, works offline).
- Link words: `server`, `lan`/`router` -> Local Wi-Fi, `wifi_direct` -> Wi-Fi Direct, `mesh` -> Mesh;
  unknown -> skipped. Blocked end -> grey dashed (existing look).
B-3 Painter
- `LinkPathsMap` / `LinkPathsCanvas` / `_LinkPathsPainter` get an optional `extraEdges` list (default
  empty) drawn with the same aerial moving line (`_flowLine` / `_dashed`), after the existing lines.
  Empty list = exactly today's drawing (offline / old server).
- This is inside the 222-locked part, so the lock hash in `link_paths_zoom_222_test.dart:100` is
  updated (listed test change; no other old test changes).
B-4 Server off -> today's view (own links only); subtitle under the title shows
  "Server off - your links only" (admin screen only). Server on -> "Who is linked to whom, live".
B-5 Log `MAP_NET_272 src=server|own nodes=N extra=E age_s=A` once per refresh change, for phone checks.

Honest limits: lines between OTHER phones come from the server, so they show up to ~30-45 s late
(phones report every 30 s + 15 s refresh) and disappear when the server is off. Your own lines stay instant.

## Part A - map readability (both maps)
- A-1 / M-1 (my advice): server lines BLUE (cloud blue `#3B82C4`) in app (`link_paths_map.dart:121`,
  above the lock) and web (`adminweb/components.js:506`); Local Wi-Fi stays teal.
- M-2 web: `user-select: none` on `.map216-stage` (`styles.css:1133`) - no blue box on "Server".
- M-3 web: phone<->phone lines bow away from the centre of the phones only, smaller bow (~0.25)
  (`views.js:1299`); server lines unchanged. Same rule for the new app phone<->phone lines.
- M-4 web chips: today they count PHONES by main link (`views.js:1281-1286`) -> no Local Wi-Fi chip.
  Option (a) show a chip when links of that kind exist; (b) keep.
- A-3 app: a phone with no link flag falls to "Mesh" (`admin_panel_screen.dart:399`); with B-2 it gets
  "Server" if the server map says online, else stays as today.

## Files if built
- App: `admin_panel_screen.dart`, `widgets/link_paths_map.dart`, `screens/link_paths_full_screen.dart`,
  new `widgets/link_map_network_272.dart`, new `test/bundle272_map_test.dart`,
  `test/link_paths_zoom_222_test.dart` (hash only), version 3.0.85+156 (4 version anchors).
- Backend (only if Part A web items chosen): `adminweb/components.js`, `adminweb/styles.css`,
  `adminweb/views.js`, new `internal/httpapi/admin_web_272_test.go`. Backend zip deployed first.
- Not changed: server API / data, curved dashed arcs, reduce motion / battery saver, photos, crown.

---
# Part C - Web "Network & mesh" page (screenshot 14 Nov): numbers that mean something else
Code: `adminweb/views.js:866-910`, `admin_panel.go:464-488`, app `presence_service.dart:108-120, 224-250`.

## N-1 (wrong data) "Mesh 1" while no phone uses mesh
- Tile = `Math.max(count('mesh'), num(data.mesh_available))` (`views.js:893`). `count('mesh')` = 0 here.
  `data.mesh_available` comes from `/admin/network` = phones whose presence says `mesh_available`
  (`admin_panel.go:483`), and the app sets that to `topology['transport_active']`
  (`presence_service.dart:115`) = "the nearby engine is running", NOT "a mesh link is in use".
- So the tile counts phones with the engine on, under the word "Mesh". Same mix for the Wi-Fi Direct
  tile (`views.js:892`, max with `wifi_direct_connected`; that one is a real WD connection, so it is right).
- Fix: Mesh tile = phones whose link is mesh + phones that have at least one mesh line in the link map;
  no `mesh_available` (engine on) in that tile.

## N-2 (misleading) "Link now" is always "Server" for a phone on the server
- Server side `admin_216.go:568-570`: an online phone always gets `link = "server"`; only phones NOT on the
  server get a Wi-Fi / WD / mesh link. So "Link now" never shows Local Wi-Fi while the server is up, even
  when the phones chat over Wi-Fi (Cycle 3 logs: all chats went over LAN).
- Fix (web only): rename column to "Main way" OR keep "Link now" and show the phone's local way when it
  has one (e.g. "Server + Wi-Fi"). Data is already in the link-map edges.

## N-3 (missing) Local Wi-Fi has no tile and "Linked with" doesn't say how
- 4 tiles: Phones online / On the server / Wi-Fi Direct / Mesh - no Local Wi-Fi, although the map shows
  Local Wi-Fi lines. "Linked with" lists names only.
- Fix: add a "Local Wi-Fi" tile (phones with at least one Local Wi-Fi line) and show the way next to each
  name: "noshad (Wi-Fi), Ibrahim (Wi-Fi)".

## Checked, not wrong
- noshad and Ibrahim show only "Sajjad" under "Linked with": edges come only from what phones report
  (`presenceNeighbors216`: direct peers seen in the last 4 min). At that moment neither noshad nor Ibrahim
  reported the other (e.g. not connected to each other then). The Map page reads the same data, so the
  two pages agree at the same moment. NOT VERIFIED whether they were linked at that time.
- Phone column "My Android Phone (26da19f3)": the real phone name + short id (admin needs it).

Files: same web files as Part A (`views.js`); no API / app change for Part C.
