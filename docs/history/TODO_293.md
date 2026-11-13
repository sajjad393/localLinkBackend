# TODO 293 - Web admin Map on phones (backend web only)

Status: BUILT (14 Nov, after "Build") -> `locallink_backend_293_2026-11-14.zip`.
Base: backend `1c4fedc` (291+292). App: untouched, no version.
Mock: `mocks/web_map_293_phone.html` (real page with the patch tried on a copy).
WIP: `~/wip_293_backend.patch` (4 files vs `1c4fedc`).

## User report
"For web is good but in mobile response have issue" (`uploads/Screenshot_20261113-174911.png`):
the map fills the phone screen, offline row cut off, Phones list hard to reach.
Choice: map a bit shorter, Phones list peeks at the bottom; one finger still moves the map.

## Causes (measured at 360x690 / 360x665, `1c4fedc`)
1. `styles.css:1137` stage `height: max(380px, calc(100vh - 150px))`. On phones the stage starts at
   y 158 (top bar + chips + legend line), so it ends at 698 > 690. In Chrome `100vh` also counts the
   address bar, so even more is hidden. Offline row (tag bottom 645..852) falls under the screen edge.
2. Stage has `touch-action: none` (kept, user choice) and fills the screen, so a finger can only pan.
   Phones panel starts at 710 (under the fold).
3. `styles.css:1185` `.map216 > .panel { height: max(380px, calc(100vh - 150px)) }` (292) comes
   AFTER the one-column rule at `:1154` (`height: auto`), so it wins: the Phones panel keeps the
   computer height on phones (515 px for 4 rows, empty space).
4. `views.js` `layout216`: at 360 px `perRow = 2`, the stagger (`xs.some` line 1308, `W` line 1295
   `+ GAP / 2`) makes the world wider than the stage; server arcs to far rows bow out of the map
   (8 phones: lines leave the right edge).
5. `views.js:1621` pill text "+N more · drag the map, or press the fit button to see everyone" is
   cut with "..." at 360 px.
6. `styles.css:1306` (<=640 px) legend gets `flex-basis: 100%`: a lone "Server" legend takes its own
   line (+28 px) and says what the chip already says.
7. Fit/zoom buttons keep a focus ring after a tap.

## Changes
### A. `adminweb/views.js`
- A1 `fitHeight293()` in mount after `stage` is found: if `window.innerWidth <= 1100` (one column)
  set `stage.style.height = max(300, innerHeight - stageTopInPage - 64)` px, so the map ends 64 px
  above the visible bottom and the "Phones (N)" header peeks. Cached per width
  (`mapView.w293/h293`) so the address bar showing/hiding does not resize it. `> 1100`: clear the
  inline height (292 CSS stays).
- A2 resize handler (line 1715): call `fitHeight293()` before `autoFit()`.
- A3 `layout216`: `stagger = perRow >= 3`. With 2 per row: no half-GAP shift, no `+ GAP / 2` in `W`,
  row step 124 (names never touch). 3+ per row (computer): unchanged.
- A4 lines stay inside the map (`if (pos.box)` guard): `edge293` = points 24 px outside the box edge every 30 px, pushed into
  `in291` for every server line; `route291` keeps 34 px from them, so a line takes a smaller bow
  instead of leaving the map. A free 217 line is unchanged (computer: identical paths, checked).
- A5 pill text on `innerWidth <= 640`: "+N more · drag the map to see them". Computer text unchanged.
- A6 legend gets class `one` (set after mount, markup unchanged for the 292 lock) when it has fewer than 2 items.
- A7 zoom buttons: `b.blur()` on click.
- A8 (user, 14 Nov: "For mobile browser little bit zoom out default map") `fit292` (line 1354):
  early `if (stageW <= 640)` branch: phone default zoom 80 - 90 % (cap 0.9, floor 0.8) instead of
  100 - 140 % (292 lines `Math.min(1.4,` / `Math.max(1, want)` kept for the computer + 292 lock). Computer (stage > 640) unchanged. + / - limits (40 - 300 %) unchanged.

### B. `adminweb/styles.css` (new 293 block after the 292 block, so order wins)
- `@media (max-width:1100px)`: `.map216 > .panel { height:auto }`, `.map292-rows { overflow:visible;
  margin-right:0; padding-right:0 }`.
- `@media (max-width:640px)`: `.map216-legend { flex-basis:auto; margin-left:0 }`,
  `.map216-legend.one { display:none }`.
- `.map216-zoom button:focus:not(:focus-visible) { outline:none; box-shadow:none }`.
- Stage line 1137 and panel line 1185 NOT edited (292 test locks them; desktop stays).

### C. Tests (new; old tests unchanged)
- `admin_web_293_test.go`: string locks for `fitHeight293`, `stagger = perRow >= 3`, `edge293`,
  short pill text, the 293 CSS block placed after the 292 panel rule.
- `testdata/map_293_smoke.js` (node, like 292): fit292 stage 330 -> k 0.8..0.9; stage 794 -> 1..1.4; layout at stage 330 px -> world width <= stage,
  no stagger; 8 phones: every server path point inside the box; stage 794 -> same positions as 292.
- Run: `node --check`, 291/292/293 smoke, `go vet ./...`, `go test -count=1 ./...`, Playwright
  harness at 360x690, 320x560, 390x800, 1352x607, 1920x960.

## Build results (14 Nov)
- `node --check views.js` OK; smoke 291 / 292 / 293 SMOKE_OK; `go vet ./...` OK;
  `go test -count=1 ./...` 6 packages ok (exit 0). Go 1.22.12.
- Mutation check: stagger forced on, phone zoom branch off, edge293 not pushed, resize without
  fitHeight293 -> each caught by the 293 tests.
- Real admin page in Chromium (faked API), base `1c4fedc` vs 293:

| Case | Base: stage / zoom / Phones top | 293: stage / zoom / Phones top | 293 pill |
|---|---|---|---|
| 360x690, 4 | 158-698 / 122 % / 710 | 130-626 / 90 % / 638 | - |
| 360x690, 8 | 158-698 / 100 % / 710 | 130-626 / 80 % / 638 | +3 more (3 outside) |
| 320x560, 4 | 158-568 / 100 % / 580, 2 hidden | 130-496 / 82 % / 508, 0 hidden | - |
| 412x800, 4 | 158-808 / 140 % / 820 | 130-736 / 90 % / 748 | - |
| 800x1000 tablet, 8 | 144-994 / 114 % / 1006 | 144-936 / 114 % / 948 | - |
| 1352x607, 4 / 12; 1920x960 | 103 % / 100 % / 140 % | same; line paths identical | same |
- No script errors. `touch-action: none` kept on the map.

## Known limits
- One finger on the map still pans it (user choice); scroll from the top bar, chips or the peek.
- Phone zoom floor 80 % keeps names readable, so with many phones some stay outside (pill counts them).
- Very short screens: map floor 300 px, so with many phones the pill counts the hidden ones.
- Rotating the phone recomputes the height (new width).
