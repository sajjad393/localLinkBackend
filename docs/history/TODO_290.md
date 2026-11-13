# TODO 290 - every admin level has its own crown colour  [BUILT 13 Nov -> app 3.0.91+162 + backend web 290, not pushed yet]

Owner (13 Nov): "I want to redesign the crown, every role have different crown color" (screenshot = web admin
Manage user > Admin level). Owner picks (ask_user):
- Colours: **Owner gold (today's crown), Operator blue, Security red, Auditor silver**.
- Where: **everywhere**: app photos, map, tags, admin screens + web admin.
- Photo ring: **same colour as the crown**.

Mock: `mocks/admin_crowns_290.html` (made by `mocks/m290/make.py`; crowns preview `mocks/m290/crowns_preview.png`).
Same crown shape as 217; only the colours change. Normal users: no change.

## Code checked (app `b1a4bbe`, backend `6e3427c`)
App - one crown drawing, gold only:
- `lib/core/widgets/admin_crown.dart`: gold constants `kCrownRing216/Fill216/Band216/Line216/Gem216`,
  `kCrownTop217/Low217/Pearl217/Blue217`; `paintCrown217()` (no role), `CrownPainter216`, `_CrownRingPainter`
  (gold ring), `AdminCrown` (photos; reads `AdminBadgeBook.instance`, which already knows the role:
  `roleOf(id)` / `adminOf(id).role`), `AdminCrown.mark()`, `AdminRoleTag216` (cream tag `#FFF6DB` /
  `#F2D27A` / text `#7A5200` for every level), `AdminRoleLine`.
- Photos everywhere go through `AdminCrown` (`account_avatar.dart`, lists, profile, calls, chat, groups).
- `AdminCrown.mark()` with no role: `account_switch_sheet.dart:183` (my admin circle, cream),
  `admin_panel_screen.dart:809` (top bar), `admin_actions_screen.dart:186` (role slot, cream),
  `admin_conversations_screen.dart:440` ("Owner only" - stays gold, correct).
- Map: `link_paths_map.dart:723` `crownKey` = ids only, `:1094` / `:1106` / `:1113` `_crown()` -> gold.
- Roles: `admin_badge.dart:64` `owner | operator | security | auditor`.
Web - one gold SVG:
- `adminweb/components.js:429` `crown216(width)` (gold), `avatar216` (`o.admin` true/false, crown + gold ring
  via `styles.css` `.av216.is-admin`), `person216`.
- `views.js:749` Admin level cards (screenshot), `:578` Users level pill `'gold'` for every level,
  `:481/:764/:976/:1402/:1432` photos (`admin: !!u.admin_role` - role is there), `:885` sessions (`s.role`).
- `app.js:300/:812` + `styles.css:1085` my own photo: gold crown + gold ring (`state.session.role` known).

## Plan (smallest)
App (version 3.0.91+162):
1. `admin_crown.dart`: `CrownColors290.of(role)` (owner = the existing constants, unchanged; operator / security /
   auditor as the mock). `paintCrown217(..., {role})`, `CrownPainter216({halo, role})`, ring colour from the role,
   `AdminCrown` passes `AdminBadgeBook.instance.roleOf(id ?? otherId)`; `AdminCrown.mark(size, {halo, role})`.
   Unknown / missing role = gold (as today).
2. `AdminRoleTag216`: colours from `level` ('Owner' / 'Operator' / 'Security' / 'Auditor'), so its callers do not
   change (`bundle216b` locks `AdminRoleTag216(level: card.roleLabel, small: true)`).
3. My own crown (switch sheet circle, admin top bar, actions role slot): my role colour + tag colours.
4. Map: `crownKey` = `id:role` pairs; `_crown(..., role)`. Repaint rule `old.crownKey != crownKey` kept (locked).
Backend web (separate zip, deployed first or together - web only, no API change):
5. `crown216(width, role)` (owner = today's SVG, unchanged strings), `avatar216` takes `admin: role` (a role
   string is still "true", old callers keep working), ring colour per role (CSS class `is-admin-<role>`).
6. Admin level cards: each level its own crown; Users pill colour per level; my own photo uses `session.role`.

## Tests
- Old locks stay as they are: owner colours `bundle217d:57-62`, `admin_web_216/217_test.go` strings
  (`#FFC727`, `#EFA300`, `#A87400`, `#E5484D`, `#FFE985`, `#3B82F6`, `function crown216`), `bundle216a`,
  `bundle216b`, `old.crownKey != crownKey`.
- New: each role gives its own fill / ring / tag colours; unknown role = gold; a photo changes colour when the level
  changes (badge book notify); map node uses the node's role; web: `crown216(12,'auditor')` has the silver colours,
  level cards show 4 different crowns, Users pill per level.
- Goldens: crowns on photos 4 roles x dark/light.

## Risks
- Red Security crown/ring next to red "missed call" text; crown is never shown on a blocked photo (unchanged).
- Phones on older versions keep drawing gold for every level until updated (the role is already in the card).

## v2 (owner 13 Nov: "crown height is too much, redesign or also can change crown")
Mock `mocks/admin_crowns_290_v2.html` (+ `mocks/m290/crowns_v2_dark.jpg` / `_light.jpg`, made by `mocks/m290/make2.py`):
today's crown (88 % as tall as wide) next to 3 lower shapes, on photos at real app sizes (radius 22 and 44):
- A Low crown - same look, 56 % (about 36 % lower).
- B Tiara - 5 small points, 44 % (half the height); small details get lost on list photos.
- C Round soft crown - rounded points, no pearls, 50 %.
A lower crown also needs less space above photos (`AdminCrown.room()` follows the height, so lists move up a little).
Plan change: new drawing in `paintCrown217` (or a new `paintCrown290`) + `CrownPainter216.heightFor()` ratio; web `crown216`
SVG the same. Old tests that lock the 0.88 ratio (`bundle217d:12` `heightFor(50) == 44`, `:32`) change (listed).
**Owner picked C - Round soft crown (13 Nov).** Full mock `mocks/admin_crowns_290.html` now uses crown C
(app chat list dark + light, profile tags, web Admin level + Users). Height ratio 0.50 (was 0.88). Waiting for "Build".

## v3 (owner 13 Nov): crown C picked + tap the crown -> tip bubble
Owner picked **C - Round soft crown** (height 0.50 x width, rounded points, no pearls).
Owner asked: "when normal user click on crown show tip message according to his/her role";
picks: small bubble above the crown, everyone sees it, everywhere the crown shows in the
APP (web admin no change), text = "what they can do for him/her".

### Tip text (from backend `internal/httpapi/admin_auth.go` adminPermission, `6e3427c`)
| role | permissions in code | tip line |
|---|---|---|
| owner | all | Developer and creator of LinkUp. Built this app so you can chat and call, even without internet. |
| operator | read, manage_devices, manage_recovery, manage_network_policy | Can help if you forget your password, lose a phone, or your Wi-Fi settings need fixing. |
| security | read, manage_recovery | Keeps LinkUp safe. Can help you get back in if you forget your password. |
| auditor | read | Checks that LinkUp works well. Can only look, can not change your account. |
| unknown | - | gold crown, title "Admin", line "Helps run LinkUp." |
Title = "Admin · <AdminBadges.roleLabel(role)>" in the crown colour (dark text colours
owner F5C542 / operator 60A5FA / security F87171 / auditor CBD5E1; light 9A6A00 / 1D4ED8 /
B91C1C / 475569). Bubble dark 2A2B35 / light white, 1 px border = ring colour 40 %.

Owner 13 Nov: owner line = "Developer and creator of LinkUp. Built this app so you can chat and call, even without internet." (picked option a); every tip says LinkUp, never LocalLink.

### Code facts (app `b1a4bbe`)
- `admin_crown.dart` `AdminCrown.decorate`: ring AND crown are wrapped in `IgnorePointer`,
  and the crown is `Positioned(top: 0.4 - h)` = OUTSIDE the Stack box (Clip.none). Flutter
  hit-tests only inside a box, so today the crown can not be tapped at all.
- `AdminRoleTag216` / `AdminRoleLine`: plain Container, no tap.
- Map `link_paths_map.dart:564`: the whole node is one `GestureDetector` -> `onNodeTap`.
- No tooltip/overlay helper in `lib/core/widgets` (only Material `Tooltip` in 2 widgets).

### Plan (smallest)
1. New `lib/core/widgets/crown_tip_290.dart`: `CrownTip290.show(context, anchorRect, role)`
   -> one `OverlayEntry` bubble (only one open; a new one closes the old). Above the crown,
   flips below when there is no room (top rows), clamped 8 px inside the screen. Closes after
   3 s, on any tap (full-screen translucent barrier that also passes nothing through) or on
   scroll / route change. 150 ms fade+scale; no animation when reduce motion is on.
   Semantics: crown = button "Admin crown, <Level>. Shows what they can do".
2. `AdminCrown.decorate` gets the role (from `AdminBadgeBook.roleOf`, same as the colour work)
   and a tap target: a transparent 32 x 32 dp area centred on the crown (crown itself stays
   painted the same). To make taps outside the photo box work, the Stack is replaced by a small
   `_CrownHitStack` that also accepts hits inside the crown rect above it (layout size unchanged).
   Callers that already reserve the room (`AdminCrownRoom`) keep the crown inside the row's box,
   so the row's own InkWell no longer gets those taps; tap anywhere else in the row = as today.
   Every caller is checked during build that its parent box covers the crown; where it does
   not, the tap area is clipped to the part that is inside (never move layout).
3. `AdminRoleTag216` / `AdminRoleLine`: tap the tag -> same bubble.
4. `AdminCrown.mark(...)` small crowns (account switch, admin top bar, actions slot):
   no tip there (they are the admin's own admin screens) - only photo crowns + tag + map.
5. Map node: the crown sits on the node photo inside the node `GestureDetector`; a tap on the
   crown area (top 40 % of the photo circle box + crown) shows the tip, the rest still calls
   `onNodeTap`.
6. Web admin: no change.

### Tests (new, nothing old removed)
- `crown_tip_290_test.dart`: tap crown on a chat row -> bubble with "Admin · Operator" + the
  operator line; second tap elsewhere closes it; auto-close after 3 s; top row flips below;
  normal user photo has no tap target; row tap still opens the chat; tag tap shows bubble;
  each role's text; unknown role text.
- Golden for the bubble (dark + light) next to the 216 goldens.

### Mock
`mocks/admin_crowns_290.html` section 5 (4 dark phones + 1 light) - made by
`mocks/m290/make.py` + `tip_part.py`; check image `mocks/m290/tip_check.jpg`.

## BUILT 13 Nov (owner: "Build") - app 3.0.91+162 + backend web 290
App (base `b1a4bbe`):
- `lib/core/widgets/admin_crown.dart`: `CrownColors290` (gold = the 216/217 constants, blue, red,
  silver; unknown = gold); `paintCrown217(..., colors:)` draws crown C (0.50 w, rounded points, no
  pearls); `CrownPainter216(colors:)` + `heightFor = 0.50 w`; ring in the level colour;
  `AdminCrown(role:)` / `AdminCrown.roleFor(id, otherId)` (from `AdminBadgeBook.roleOf`);
  `AdminCrown.mark(size, role:)`; `decorate(role:, tip: true)` adds the crown tap area
  (max(32, w+12) x max(24, h+12), bottom 4 px into the photo); `AdminRoleTag216` colours from `level`
  + tap = tip.
- New `lib/core/widgets/crown_tip_290.dart`: `CrownTapScope290` (one, in `_look290` round every
  MaterialApp) lets the crown areas that stick out above photos take the tap, only when visible
  (paintsChild + every clip), on the current route, and nothing else drawn on top (app bar, sheet, later
  map node...); the normal hit path is still added behind it, so a drag on a crown scrolls. 
  `CrownTip290` bubble in the root overlay: above the crown, below the photo on top rows, 8 px inside
  the screen; closes after 3 s, on any new finger, when its screen closes; second tap on the same
  crown closes it; no fade when the phone asks for less motion.
- Roles passed: `AdminPersonAvatar216(adminRole:)`, `ServerPersonAvatar216(adminRole:)`, admin Users
  rows + sheet (`admin_role`), `LinkNode.adminRole` (link map from server rows), node sheets, my hero
  card, account switch circle (`s.role`), admin top bar (`session.role`), Admin actions slot
  (`widget.role`). Admin conversations "Owner only" slot stays gold. Map nodes: photo crowns via
  `AdminPersonAvatar216` (level from the admin cards). `link_paths_map.dart` below "Bundle 217: when the
  map lines may move" is NOT touched (sha256 lock in `link_paths_zoom_222_test`); its old canvas crown
  (`drawNodes: true`, not used on screen since 216) stays gold.
- Version 3.0.91+162 (`app_build.dart`, `pubspec.yaml`, `bundle223_logs_test`, `nearby_190_test`).
- Old test changed (shape C only): `bundle217d_test.dart` height 0.88 -> 0.50 (`heightFor(50)` 44 -> 25,
  `mark(20)` height 17.6 -> 10.0). Nothing else changed or removed.
- New `test/crown_tip_290_test.dart` (13 tests). 4 tap tests FAIL with the scope switched off.
Backend web (base `6e3427c`): `components.js` `CROWN_290` + `crownRole290` + `crown216(width, role)`
(crown C), `avatar216` `admin: role` -> `is-admin-<role>`; `styles.css` ring per level; `views.js`
photos pass `admin_role`, level cards `crown216(12, value)`, Users pill tone per level; `app.js` my
identity crown + menu photo use `session.role`. New `admin_web_290_test.go`.
