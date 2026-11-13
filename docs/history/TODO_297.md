# TODO 297 (BUILT 14 Nov -> locallink_backend_297_2026-11-14.zip, on backend 775c3d8)

Backend only (web admin `internal/httpapi/adminweb`). App unchanged. Base: backend `775c3d8`.

## 297-A Phones list cuts the left side of the role ring (screenshot 14 Nov)
- Cause (code): `.map292-rows { overflow-y: auto; ... }` (styles.css:1187). `overflow-y: auto` also
  clips sideways. The ring is a `box-shadow` 2.6 px OUTSIDE the photo (`.av216.is-admin*`,
  styles.css:1117-1122), and the photo starts at the list's left edge, so 2.6 px on the left are cut
  (visible on ayesha, kiran, sajjad, sumaira).
- Fix: `.map292-rows { padding-left: 4px; margin-left: -4px; }` (rows stay where they are, the ring has
  room). Same check for the top crown of the first row.
- Phones / tablets (<= 1100 px) use `overflow: visible`, so they are not affected.

## 297-B Full screen map
- New button "Full screen" on the map. Click: only the map is shown, filling the whole screen.
  Sidebar, top bar, filter chips and the Phones panel are hidden.
- An "Exit full screen" button (top right of the map, X + words) brings everything back.
  Esc does the same.
- Uses the browser's real full screen where it works (also hides the browser bars). Where it does
  not work (iPhone Safari), the map still fills the whole page (fallback).
- The map fits everyone again on enter and exit. Live updates, zoom (+ / -), drag and pinch keep working.
- User choices (14 Nov): real full screen (iPhone: fills the page) | only map + zoom buttons + Exit |
  button under the zoom buttons (corners icon), Fit gets a target icon | mock first.
- Mock: `mocks/web_map_297.html` (real admin page, fake data, change on a copy).
- Code notes: the Map re-renders on every live frame (`mount(..., true, msg)`), so the full screen
  state is kept in `mapView.full297` and put back after each render; `fullscreenchange` keeps it in
  step when the browser leaves full screen (Esc / back gesture); the map is fitted again on enter/exit.

## Not changed
- Map layout (292-294), live data (296), lines, colours, other pages, the app.

## Tests
- Go test with string locks for the CSS fix, the button, the exit button and Esc.
- Smoke test (jsdom) for enter / exit.
- Chromium check at 1341x597 (your screen), 360x690 (phone), 1920x960.


## Build notes (14 Nov)
- The whole page goes into browser full screen (`document.documentElement.requestFullscreen`), not the
  map box: the map box is drawn again on every live frame, and a removed full screen element would
  drop out of full screen. CSS `.map216-stage.full297` (fixed, inset 0) shows only the map; the
  same CSS is the fallback when there is no full screen API (iPhone Safari).
- `mapView.full297` keeps the state across live redraws (the class is in the template).
- Esc, the Exit button, the browser's own exit (`fullscreenchange` / `webkitfullscreenchange`,
  added once per page) and opening another page (`leaveFull297` from app.js) all end it.
- The full screen buttons are skipped by the drag code (found in the real build test: the
  pointer capture swallowed the click).
- Re-fit 120 ms after enter and exit.
