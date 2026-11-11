# TODO 267 - Add Contact: people to add by default, Nearby only when search finds no one

Status: BUILT as 3.0.83+154 (14 Nov) + backend 267.
Base: 3.0.80 (`03a4841` local = GitHub `9d223ef` + `wip_3.0.80_app.patch`). Backend base `ca4f41c`.
Can be built alone or after 265/266 (no shared files with 266; 265 touches none of these).

## User words (13 Nov)
- "Show Nearby list only if user not found in search, don't show by default.
  By default show all friends on Add Contact screen."
- Who: "If connected to the server show all who are not in my contacts. If not connected
  to the server show all who are not in my list but are in my nearby device contacts."
- Nearby: start it only after a failed search (option 2) "but show some animation when
  finding and a message please wait".

## Today (checked in code)
- `add_contact_screen.dart` `_buildResults` (:341-445): empty box -> the Nearby section is
  ALWAYS shown (title "Nearby"), and it starts a Wi-Fi Direct search on open
  (`nearby_people_section.dart:195-197` `_startSearch` -> `discoverWifiDirect`).
  Typing -> results + Nearby filtered by name; no match -> "People nearby" with everyone.
  Without the nearby engine: "Type at least 2 characters to search."
- Server: only `GET /api/v1/directory/name?q=` (`directory.go:61`, LIKE, max 50). No "list
  everyone" call.
- Phone: `directory_profiles` table (`local_store.dart:580`) already keeps every profile
  this phone learned: from nearby friends' contact lists (gossip `DIRECTORY_SYNC_REQUEST`,
  `source` default `mesh`), from nearby phones (`nearby`), QR (`qr`), server (`server`).
  Private contacts of friends are never shared (bundle 90), so they are not in it.

## A. Backend (zip, deploy FIRST; old apps unaffected)
A1. New `GET /api/v1/directory/people?offset=0&limit=50` (`internal/httpapi/directory.go`,
  route in `cmd/server/main.go` next to `/directory/name`).
- Same columns and same rules as `DirectoryByName` (active user, `directory_sync_enabled=1`,
  `discoverable_by_name=1`, phone hidden unless public) PLUS:
  not the requester, not already in the requester's `account_contacts`.
- `ORDER BY display_name COLLATE NOCASE`, `limit` 1-50 (default 50), `offset` >= 0.
- Answer `{"profiles":[...], "more": true|false}`.
- Same privacy as today: anyone could already list these people by searching one letter.
A2. Tests `directory_people_267_test.go`: contacts excluded, self excluded, hidden
  (`discoverable_by_name=0`) excluded, paging + `more`, 401 without login.

## B. App - data
B1. `locallink_api.dart`: `directoryPeople({offset, limit})` -> `(List<LocalProfile>, bool more)`.
  404 (old server) -> throws `ServerTooOld267`.
B2. New small file `lib/features/directory/data/people_to_add_267.dart`:
  `PeopleToAdd267.load(repo, {offset})` -> `(list, more, source: server|nearby)`.
  - Server reachable (`ServerReach` not skipping, socket/HTTP ok) -> server page.
    The phone also removes its local contacts, blocked people and me (same checks as
    `searchByName` :1916-1921) and saves each row like a search result (`_saveServerProfile`).
  - Server not reachable / old server / error / 4 s timeout -> local list:
    `directory_profiles` rows with `source != 'server'`, not in `contacts`, not blocked,
    not me, `discoverable_by_name=1`, `directory_sync_enabled=1`, name A-Z, max 200.
    (= people my nearby phones / friends shared.) Also starts one `repo.sync()` (has its own
    cooldown) so fresh rows from nearby friends arrive; the list reloads when it ends.
  - Implemented as an extra interface on `DirectoryRepository`, so test fakes that only
    implement the old contract keep working (they simply get an empty list).
B3. Log (no names): `ADD_LIST_267 src=server|nearby shown=N more=y|n ms=..`.

## C. App - screen (`add_contact_screen.dart`)
C1. Empty search box -> list "People you can add" (server) or "People nearby phones know"
  (offline), each row = the existing `SearchResultCard` (avatar, name, @username, status,
  Add button; tap = profile). Server: next 50 load when the list end is reached.
  - Loading: small spinner row. Empty server list: "Everyone is already in your contacts."
    Empty offline list: "No one to show yet. Search a name or scan a QR code."
  - Pull down = reload. After Add -> the person leaves the list (`_loadContacts` already runs).
C2. Typing (2+ letters) -> search exactly as today (results, blocked matches, offline hint).
  The default list is hidden while typing. Nearby is NOT shown while results exist.
C3. Search found no one (`noMatch`) -> "No one called "x" found." then the Nearby section
  is created at that moment (title "People nearby", filter '') -> it starts its search then.
  While no phone is found it shows the existing search animation with
  "Looking for LinkUp phones…" + NEW line "Please wait a few seconds" + "Both phones need
  Wi-Fi on" (existing words kept - tests lock them). Allow / Bluetooth / Wi-Fi cards stay.
  - Clearing the box or typing a name that matches -> Nearby goes away (search stops).
C4. `NearbyPeopleSection`: new optional `waitLine` (default null) used for C3; no other change.
C5. Server state changes while the screen is open (connectivity stream) -> the default list
  reloads from the new source once.

## D. Not changed
- Search logic, QR scan, My QR, profile page, Add flow, friend requests, Nearby cards.
- Contacts tab, Chats tab.

## E. Tests
New `test/bundle267_test.dart`:
1. Empty box + server list -> rows shown, no Nearby section, no Wi-Fi Direct search call.
2. Server list skips my contacts / blocked / me even if the server sent them.
3. Server fails -> local list: only rows not from server, not contacts, not hidden.
4. Typing with matches -> results only, no Nearby.
5. Typing with no match -> "No one called" + Nearby section + "Please wait a few seconds".
6. Clear the box after a miss -> Nearby gone, default list back.
7. Paging: second page appended, stops when `more=false`.
8. Fake repo without the new interface -> empty-list text, no crash.
9. `ADD_LIST_267` log line has no names.
Old tests changed: `bundle170_test.dart:142` ("Type at least 2 characters to search." ->
the empty-list text), version lines in `bundle223_logs_test` + `nearby_190_test`.
Backend: new Go test file only.

## F. Phone test
1. Server on: open Add Contact -> list of people not in contacts, no Nearby, no animation.
2. Search a name that does not exist -> "No one called…" + animation + "Please wait…",
   nearby phones appear.
3. Server off (stop the PC server): open -> people your nearby friends know; search miss -> Nearby.

## G. Delivery
Backend zip `locallink_backend_267_<date>.zip` (deploy first) + ONE app zip + ONE commit `v3 3.0.8x+15x bundle 267 …`.
