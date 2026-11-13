# TODO 294 - Web admin Map: reached phones, lines off crowns, fewer crossings

Status: BUILT (14 Nov, after "Build") -> `locallink_backend_294_2026-11-14.zip`.
Base: GitHub `da74731` (backend 293, pushed; identical to the 293 zip).
App: untouched, no version. Mock: `mocks/web_map_294.html`. WIP: `~/wip_294_backend.patch`.

## User report (screenshot `uploads/image-14.png`, 1341x597, 103 %)
Answers: a phone reached over Wi-Fi Direct / mesh goes "in the normal rows, in colour, not grey";
fix "all 4 issues".

## Causes (line numbers = 293 zip `adminweb/views.js`)
1. Offline row: `layout216` 1300-1301 `online = nodes.filter((n) => n.online)` / `!n.online`; same test in
   the keep-spot check 1523 / 1526 (`o.online === !!n.online`), the grey class 1572
   (`n.online ? '' : 'off292'`) and the OFFLINE line 1579-1580. `n.link` (`wifi_direct`, `mesh`, `lan`)
   is never looked at, so sumaira (link `wifi_direct`, edge sajjad->sumaira) is grey under OFFLINE while the
   chip, legend and Phones list say Wi-Fi Direct.
2. Line through a crown: `.map216-node` (styles 1141) is a column photo 48 + gap 6 + tag, centred on the
   spot with `translate(-50%,-50%)`. So the photo spans spot-38 .. spot+10 and the crown sits above it up to
   about spot-50. `route291` (1464) keeps `R = 34` px from the spot only, so a line can cross the top of the
   photo and the crown.
3. Crossings: `route291` checks phones (34 px) and line middles (16 px), never crossings. Edges are routed
   in API order. A phone right under the server gets the 0.60 bow first (`arc217(p, q, C, 0.60)`), which
   for a near-vertical line is a big sideways loop (server->kiran in the screenshot) that then crosses the
   ayesha-sajjad line.
4. Bunching: nothing compares how two lines arrive at the same phone (ayesha->sajjad and kiran->sajjad
   arrive from nearly the same side).

Reproduced in the real page (fake data = the screenshot's 4 people and 6 links): sumaira grey, OFFLINE line,
server->kiran loop crossing the orange line, orange line over kiran's crown.

## Changes (`internal/httpapi/adminweb/views.js` only)
- A. `function reach294(n)` = `n.online || (n.link && n.link !== 'offline')`. Used in `layout216`
  (rows: reached phones in the normal rows after the server ones; the order is already rank291: server,
  links, blocked, offline), keep-spot check + stored state, the grey class `off292`, the OFFLINE line.
- B. Obstacles: for every other phone, also a point 40 px above the spot with radius 24 (photo top + crown).
  `route291` takes `o.r` (own radius) when given; old obstacles keep 34.
- C. Crossings:
  - `route291(p, q, C, ks, first, obstacles, mids, drawn)`: new optional `drawn` (lines already placed).
    Each try counts crossings with drawn lines (lines that share a phone do not count; quick skip when the
    boxes do not touch; 12 pieces per curve).
  - Pick order: not under a phone > fewer crossings > no bunching > middles apart > the usual bow first.
    The first try that is clear on all of them is taken at once (an uncrowded map keeps the 217 look).
  - Server lines are placed first, phone lines after them (only the routing order; drawing order and the
    moving dots stay as they are).
  - A phone more or less under the server (`|dx| < 0.6 * |dy|`) tries the gentle bows first
    `[0.15, 0.35, 0.60, 0.85]`.
  - Phone lines get one more, bigger bow as the last try `[0.25, 0.45, 0.10, 0.65]`, so a line can pass
    under a phone in between instead of crossing its server line (found in the build test).
- D. Bunching: a try that reaches a phone less than 22 degrees from a line already at that phone loses to one
  that does not.

## Tests
- Old test changes (listed):
  - `admin_web_291_test.go` 21-23: the `route291` signature / call strings get `, drawn` / `, drawn294` and
    the gentle-first list for steep server lines, phone list `[0.25, 0.45, 0.10, 0.65]`.
  - `admin_web_291_test.go` 33 and `admin_web_292_test.go` 31: `!!n.online` / `n.online ? '' : 'off292'` ->
    `reach294(n)`.
  - `testdata/map_291_smoke.js`, `map_292_smoke.js`, `map_293_smoke.js`: add `'reach294'` to the list of
    grabbed functions (one word each; checks unchanged).
- New `admin_web_294_test.go` (string locks) + `testdata/map_294_smoke.js` (real code in node):
  - screenshot case: sumaira in the online row, no OFFLINE line; ayesha-sajjad line keeps >= 24 px off
    kiran's crown point; server->kiran gentle (no loop); 0 crossings; lines into sajjad >= 22 degrees apart.
  - a phone with `link: 'offline'` stays grey under OFFLINE; `online: false, link: 'mesh'` is not grey.
  - forced crossing case (a-c and b-d with b between): exactly 1 crossing, not more.
  - old 291 cases (free line keeps 0.60, same line twice gets 2 bows) still pass.
- Run: `node --check`, smoke 291-294, `go vet ./...`, `go test -count=1 ./...`, Playwright harness
  (real page) 1341x597 + 360x690 with the screenshot data and an 8-phone mixed map; mutation check.

## Build results (14 Nov)
- `node --check` OK; smoke 291 / 292 / 293 / 294 SMOKE_OK; `go vet ./...` OK; `go test -count=1 ./...`
  6 packages ok (exit 0); gofmt clean on the test files.
- 294 smoke: sumaira in the normal row at 794 / 1100 / 330 px; 0 crossings; lines keep >= 44 px off other
  crowns; lines into sajjad 40-60 deg apart; first row of the screenshot (kiran in the middle, 4th phone not
  reached): ayesha-sajjad goes under kiran (44 px off), 0 crossings; phone under the server: 13 px sideways;
  forced case: 1 crossing.
- Mutation check (7 cuts: reach, crossings in the rank, early stop, crown point, bunching, gentle list,
  own radius): each caught by the 294 tests.
- Real page in Chromium (faked API), base `da74731` vs 294, 1341x597 / 360x690 / 1920x960, 3 maps:
  no script errors; grey: screenshot map none (was sumaira), mixed map only ali + raza (was 4);
  fit / zoom buttons work.
- Speed (node, this machine): 8 phones 11 lines 11 ms; 30 phones 46 lines 67 ms per redraw.

## Known limits
- When a line has to pass between two others it still crosses (as few times as possible).
- Very big maps (30+ phones with many links) redraw a bit slower (about 0.1 s here, more on a slow PC).
- The computer picture changes where lines crossed or looped before; uncrowded maps look the same.
