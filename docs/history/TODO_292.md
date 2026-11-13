# TODO 292 - web Map: fill the screen, readable default zoom  [BUILT -> locallink_backend_292_2026-11-14.zip (on 69f43c8, carries 291 too); see APPLY_backend_292.md]

Owner (13 Nov, image-13): "there are a lot of empty space in map bottom, redesign this and show me the mock.
Default zoom make at least name is clearly readable."
Mock: `mocks/web_map_292.html`, approved. Answers: design OK -> TODO; offline = grey row (my advice);
too many phones = keep 100 % + "+N more" pill (my advice).
Base: backend 291 zip (on `6e3427c`, carries 290). Web only: `internal/httpapi/adminweb/views.js`, `styles.css`.
App not touched. Version: backend 292 (no app version).

## Why it looks like this (VERIFIED in code, 291 tree)
- `styles.css:1137` `.map216-stage { height: clamp(320px, 52vh, 620px) }`. Owner's window is 607 px high
  -> 52vh = 316 -> 320 px map, ~140 px empty page under it.
- `views.js` `layout216` (:1285): fixed world 1000 x 700, server (500,110), online phones on a circle
  330 x 300, offline phones in a row at y = H - 90 = 610.
- `fit217` (:1330ish) fits everybody (60 px sides/top, 80 px bottom), zoom 0.4 - 1.6. Box = y 50..690 = 640 px
  high in a 320 px stage -> zoom 0.5 -> name tag 11 px (`styles.css:1142`) shows at 5.5 px.
- The legend sits inside the map, bottom left (`styles.css:1146`), so sumaira (offline row) lands on it.

## Changes
1. **Map height** (`styles.css`): `.map216-stage` height `max(380px, calc(100vh - 150px))` (457 px on the
   owner's screen). The Phones panel next to it gets the same height; its list scrolls inside, the
   "Click a person ..." line stays at the bottom. The <= 1100 px one-column rule and the <= 640 px rules stay.
2. **Layout** (`layout216`, same name so the 274/291 code and smoke keep working):
   - server top centre (y 58); online phones in a row under it, 130 px apart, middle a little lower
     (26 px bow); more than fit in the stage width (at 100 %) -> next row, 138 px lower.
   - offline phones: one dashed line "OFFLINE" (10 px caps, tertiary colour), then the offline row(s), 112 px apart.
   - world width/height come from the rows (not fixed 1000 x 700); `apart291` takes those bounds instead of
     its own `W = 1000; H = 700`.
   - fixed 291 order stays (server phones, other links, blocked, offline; then name). The W-2 rule (a redraw keeps
     phones where they were) and the 291 keep-apart + line rules stay. Lines unchanged: server 0.60 first
     (then 0.35 / 0.85 / 0.15), phone lines 0.25 first.
   - stage width for the rows = last measured `stage.clientWidth` (first draw: page width - side panel).
     Window resize only fits again (no new layout, phones do not jump).
3. **Default zoom** (new `fit292`, used on first draw, filter change, resize):
   zoom = everybody's fit, kept between **100 % and 140 %** (names >= 12 px). If everybody does not fit at
   100 %: zoom 100 %, centred, server 18 px from the top.
   - "+ / -", wheel, pinch and the fit button stay as they are (fit button = `fit217`, 40 % - 160 %, everybody).
   - New small zoom text under the 3 buttons ("100%"), 10 px, tertiary colour; updates on every zoom.
4. **"+N more" pill**: when shown phones are outside the visible map, a pill at the bottom centre:
   "+N more · drag the map, or press the fit button to see everyone". Updates on drag / zoom; gone when N = 0.
5. **Legend**: out of the map, into the top bar (`bar216`), right side, same items
   (Server / Wi-Fi Direct / Mesh / Local Wi-Fi / "Blocked · relays only"). Phone width: it wraps under the chips.
6. **Name tag** 11 -> 12 px (`.map216-node .tag`). Offline phones: photo grey + 55 % opacity.

## Tests
- New `admin_web_292_test.go`: string locks (stage height rule, `fit292`, 100 % floor `Math.max(1,`,
  legend inside `bar216`, "OFFLINE" line, "+N more" text, tag 12 px) + node smoke:
  4 / 12 / 30 phones at stage 794 x 457: default zoom >= 1.0; 4 phones: everybody visible, zoom <= 1.4;
  12 / 30: pill count = phones outside; min gap >= 110 px; offline row below every online phone.
- Changed (listed here only): `testdata/map_291_smoke.js` "inside" check uses the bounds `layout216` returns
  (was fixed 60..940 / 190..630).
- Old locks stay: `fit217(`, `arc217(p, q, C, 0.60)`, `arc217(p, q, CP, 0.25)`, `data-map-zoom="fit"`,
  "Fit everyone", "Blocked \u00b7 relays only", `.map216-stage {` with `user-select: none`, 216/217/272/274/290/291.
- `node --check`, `go vet ./...`, `go test ./...`.

## Not in this TODO
- App maps (291 D already keeps them apart). Map colours, line motion, crowns.
