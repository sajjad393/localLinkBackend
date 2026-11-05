# TODO 219 — Web admin panel: refresh fixes (menu button + shimmer instead of login flash)

Status: BUILT (6 Nov 2026) after "go ahead" + "mocks OK". Q1 = A, Q2 = A.
Repo: backend only (`sajjad393/localLinkBackend`). The phone app is not touched.
Base: backend after bundle 218 is pushed (218 zip applies on `6c82546`; 219 goes on top of the 218 commit).
Files (only these): `internal/httpapi/adminweb/index.html`, `adminweb/styles.css`, `adminweb/app.js`,
`adminweb/components.js`, `adminweb/views.js` (added while building, see §3 219d), new `internal/httpapi/admin_web_219_test.go`, `APPLY_219.md`, `TODO_219.md`.

---

## 1. What you reported (Chrome on the phone, 6 Nov 2026)

| # | What you see | Screenshot |
|---|---|---|
| R1 | After signing in, the top bar is perfect: ☰, title, ↻, bell, photo with crown. | 00:21 |
| R2 | After a refresh (pull down to refresh, or the reload button), the ☰ button is gone. | 00:13 |
| R3 | After a refresh, the login page shows for about a second, then the data comes. | — |
| R4 | Your choice: show **shimmer** instead of "Loading…". | — |

## 2. Why it happens (checked in the code)

### R2 — ☰ missing
- `index.html:142` — the ☰ button starts with class `hidden`:
  `<button class="btn-icon hidden" id="menu-toggle" …>`
- `app.js:1133-1138` — the only code that removes `hidden` runs inside `window.addEventListener('resize', …)`.
- After you sign in, the keyboard closes and that changes the screen size, so `resize` runs and ☰ appears (R1).
- A refresh has no keyboard and no size change, so `resize` never runs and ☰ stays hidden (R2).
- Same root cause, also hidden today: on the phone the drawer still shows "Collapse menu" after a refresh (`collapse-toggle` is only hidden by the same resize code).
- Note: the panel's own ↻ button (`refresh-button`, `app.js:633 refreshAll`) does not reload the page and never touches ☰.
  The ☰ loss comes from a real page reload (pull-to-refresh or Chrome's reload). Phone test 219-2 checks both anyway.

### R3 — login flash
- `index.html:65` — the login page (`<main id="auth" class="auth">`) is visible by default in the HTML; `#app` starts `hidden` (`index.html:119`).
- `index.html:186-189` — every script has `defer`, so Chrome paints the HTML (= login page) before any JS runs.
- `app.js:1293-1305 boot()` — with a saved login it first waits for `GET /api/v1/admin/summary` and calls `showApp()` only after the answer.
  The login page stays on screen during the whole wait.
- `app.js:1303` — `catch (err) { showLogin(); }`: any failure (a Wi-Fi drop, server restarting, a timeout) sends you to the login page, although your login is still good.
- 429 (too many requests): `noteRateLimit()` (`app.js:109`) keeps the login and shows a "throttling" banner. **Correction (found while building):** the first version of this TODO said it deletes the login; it does not. The real 429 problem is only at refresh: `boot()`'s catch shows the login page on any error, 429 included. (Q1.)

## 3. Phases

### 219a — ☰ always right (CSS first, no waiting for JS)
1. `index.html:142` — remove `hidden` from `#menu-toggle` (keep id, aria-label, title as they are).
2. `styles.css` — the layout decides, using the same 1000 px breakpoint as `styles.css:822` / `:1205`:
   ```css
   /* Bundle 219: the menu button follows the screen width, also right after a refresh. */
   #menu-toggle { display: none; }
   @media (max-width: 1000px) {
     #menu-toggle { display: inline-grid; }
     #collapse-toggle { display: none; }
   }
   ```
3. `app.js:1133-1138` — keep the resize listener, but it only closes the drawer when the screen becomes wide
   (`window.matchMedia('(max-width: 1000px)')`, so JS and CSS use the same rule, not `innerWidth < 1000`).
   Stop toggling `hidden` on `menu-toggle` / `collapse-toggle` (CSS does it now).
4. Add a `change` listener on that `matchMedia` (rotate phone, split screen) and a `pageshow` listener (Chrome "back" from cache) that run the same drawer close.
5. Nothing else in the top bar changes (217 compact layout stays exactly as in R1).

### 219b — Shimmer from the first frame (no login flash)
1. `index.html` — `#auth` starts hidden too: `<main id="auth" class="auth hidden">`.
2. `index.html` — add a static boot shell right after `<body>`'s svg defs, visible by default, pure HTML + CSS (works before JS):
   - `<div id="boot219" class="boot219" aria-busy="true" aria-label="Loading LocalLink Admin">`
   - phone (≤1000 px): top bar = ☰ grey square 36, title bar 90×22, then on the right a circle 28 (bell) and a circle 40 (photo).
   - computer: a 240 px grey sidebar with 7 grey rows + the same top bar.
   - body: 1 title bar + 3 grey blocks (generic shape).
   - all blocks use the existing `.skeleton` class (`styles.css:651`) so they shimmer and turn still with reduce motion (`styles.css:882`).
3. `app.js boot()`:
   - **no saved login** → hide `#boot219`, `showLogin()` (logged-out users see the shimmer only for a few ms).
   - **saved login** → hide `#boot219`, show the real app shell **at once**, before the server answers:
     - split `showApp()` (`app.js:671`) into `paintShell219()` (top bar, name, role, "A" photo + crown from the saved identity, nav, crumbs of the last page) and `mountView(start)`.
     - put the page-shaped shimmer (219d) into `#view` with the last page's title in the top bar.
     - then call `/api/v1/admin/summary`; on success mount the page; the real data replaces the shimmer in place (no jump of the top bar).
4. `#auth` is shown only by `showLogin()`; nothing else shows it.

### 219c — Don't sign out by mistake
1. `boot()` catch (`app.js:1303`):
   - **401** → `api()` already calls `handleExpiry()` → login page with "Your admin session expired. Sign in again." (unchanged).
   - **network error / 5xx / no answer in 10 s** (an `AbortController` with 10 s on the boot call only) →
     keep the shell + shimmer, show a bar under the top bar:
     **"Can't reach the server · Retry"** (amber, icon `i-pulse`, button "Retry").
     - auto retry after 5, 10, 20, 30, 30 … seconds; "Retry" tries at once.
     - when it works, the bar goes away and the page loads. The saved login is never deleted here.
   - **429** → see Q1.
2. The same bar is reused if the first `mountView` fails with a network error at boot (instead of the red error panel).
3. Wording (English, plain): `Can't reach the server` · `Retry` · `Trying again in 10 s…` · `Server is busy. Trying again in 30 s…` (Q1).

### 219d — Shimmer shaped like your page
`components.js` — new `ui.bootSkeleton219(viewId)` (next to `skeletonPanel` at `components.js:295`):

| Page (`views.js` id) | Shimmer shape |
|---|---|
| `map` | 2 chip pills (All / Server), one big rounded box (phone 300 tall, computer 420), legend line, "Phones" panel with 2 rows: circle 40 + 2 lines + pill on the right |
| `dashboard` | 4 number cards (`skeletonCards(4)`) + 1 chart box (`h-xl`) + 1 list panel |
| `users`, `devices`, `sessions`, `audit`, `security`, `recovery` | search bar + 5 rows; phone = 5 cards (circle + 2 lines), computer = 5 table rows |
| everything else | `skeletonPanel(5)` (today's shape) |

- Light and dark theme use the theme colours (`--surface-muted`, `--border`).
- Reduce motion → still grey, no shimmer (existing rule `styles.css:882`).
- See Q2 for using the same shapes when you switch pages.
- Added while building: 8 views (`dashboard`, `users`, `devices`, `sessions`, `security`, `audit`, `recovery`, `map`) paint their own plain `skeletonPanel(N)` while they load (`views.js:118, 490, 635, 693, 913, 990, 1033, 1142`). Each now calls `ui.bootSkeleton219('<id>')`, otherwise the shimmer would jump shape halfway.

## 4. Decisions (6 Nov 2026)
- Q1 = **A**: on 429 keep the login, keep the page, show "Server is busy. Trying again in N s…" and retry by itself.
- Q2 = **A**: page-shaped shimmer on refresh **and** on page switch (`mountView`, `app.js:611` uses `ui.bootSkeleton219(viewId)` instead of `ui.skeletonPanel(5)`).

### Original questions
- **Q1 — 429 (too many refreshes).** Today it deletes your login and says "session expired".
  - A: Keep you signed in; keep the shimmer/data; show "Server is busy. Trying again in N s…" and retry by itself. **(my advice)**
  - B: Leave it as it is today.
- **Q2 — Page switch.** When you tap another page in the menu, today it shows 5 plain grey lines.
  - A: Use the same page-shaped shimmer as on refresh. **(my advice)**
  - B: Only on refresh.
- **Q3 — Do what you think best.**

## 5. Mocks (made before the build, PNG + one tall JPG)
- `mock_219_web01.png` — phone, dark, Map page refreshing: full top bar with ☰, map-shaped shimmer.
- `mock_219_web02.png` — phone, light, Dashboard refreshing: number cards + chart shimmer.
- `mock_219_web03.png` — phone, dark, "Can't reach the server · Retry" bar over the shimmer.
- `mock_219_web04.png` — computer 1280 px, Users page shimmer with sidebar (no ☰, "Collapse menu" visible).
- `mock_219_all.jpg` — all four in one tall picture.
The build must match the mocks 100%.

## 6. Tests
New `internal/httpapi/admin_web_219_test.go` (Go, reads the embedded files like `admin_web_217_test.go`):
1. `index.html`: `id="menu-toggle"` has no `hidden` class.
2. `index.html`: `<main id="auth" class="auth hidden">` and `id="boot219"` with `aria-busy="true"` exist.
3. `styles.css`: has `@media (max-width: 1000px)` with `#menu-toggle { display: inline-grid; }` and `#collapse-toggle { display: none; }`.
4. `app.js`: `boot()` no longer has `catch (err) { showLogin(); }`; has `paintShell219`, `AbortController`, `Can't reach the server`.
5. `app.js`: the resize code no longer toggles `hidden` on `menu-toggle`.
6. `components.js`: `bootSkeleton219` with the `map`, `dashboard`, `users` shapes.
7. (Q1-A) `app.js`: the 429 path does not remove `TOKEN_KEY`.

jsdom check (`p219/harness219.mjs`, like `p217/harness217.mjs`, not shipped):
- saved login + summary delayed 2 s → `#auth` hidden the whole time, `#app` visible, `.skeleton` in `#view`, top bar name = saved name.
- summary 401 → login page with the expiry message.
- fetch rejects → still in the app, bar "Can't reach the server" visible, Retry calls summary again; next success hides the bar.
- no saved login → login page, `#boot219` hidden.
- (Q1-A) summary 429 → still signed in, "Server is busy" bar.

Old tests stay unchanged (`admin_web_test.go:37` only needs `id="auth"` and `id="app"` to exist — still true).
Full `go test ./...` must pass.

## 7. Done when
- [ ] Phone Chrome: after any refresh the top bar is the same as after sign-in (R1), ☰ included.
- [ ] Refresh with a saved login never shows the login page, not even for a moment; you see shimmer shaped like the page, then data.
- [ ] Wi-Fi off + refresh → still in the panel, "Can't reach the server · Retry"; Wi-Fi on → data comes back by itself.
- [ ] Only an expired login (401) shows the login page.
- [ ] Computer width: no ☰, sidebar with "Collapse menu"; phone width: ☰, no "Collapse menu" in the drawer.
- [ ] Reduce motion on → grey blocks without shimmer.
- [ ] Build matches `mock_219_*` 100%.

## 8. Phone tests (Realme, Chrome, `http://192.168.18.27:8080/admin`)
- 219-1 Sign in → top bar complete (☰ ↻ bell photo+crown). Screenshot.
- 219-2 On Map: pull down to refresh 5 times, then Chrome ⋮ → reload 2 times, then the panel's ↻ 2 times → ☰ always there, login never flashes.
- 219-3 Same on Dashboard and Users (shimmer shape matches the page).
- 219-4 Rotate the phone to landscape and back → ☰ / drawer correct.
- 219-5 Wi-Fi off → refresh → bar "Can't reach the server". Wi-Fi on → panel loads alone within 30 s.
- 219-6 Stop the server → refresh → bar; start the server → panel comes back, still signed in.
- 219-7 Sign out → refresh → login page (no shimmer stuck).
- 219-8 Computer browser (Ctrl+F5): no ☰, sidebar normal.

## 9. Build order (after "go ahead 219")
1. Mocks `mock_219_web01..04` + `mock_219_all.jpg` → you check them.
2. 219a (CSS + markup) → 219b (boot shell + `paintShell219`) → 219c (bar, retry, Q1) → 219d (shapes, Q2).
3. Go test file + jsdom harness → `go test ./...` green.
4. `ui_check_219.jpg` (built vs mock) → `locallink_backend_219_<date>.zip` + `APPLY_219.md` + PowerShell.
5. You: push 218 first (backend + app), then this zip → commit → deploy → Ctrl+F5.
