# APPLY 219 — Web admin panel: refresh fixes

Backend only (the web panel is inside the server). The phone app is not touched.
Apply **after** bundle 218 is committed and pushed (this zip contains 218's `views.js`/`styles.css` plus the 219 changes).

## What you see now
- **☰ after a refresh**: the menu button is shown by the page layout itself (≤ 1000 px wide), so it is there straight after a pull-to-refresh or reload. On a computer: no ☰, the sidebar and "Collapse menu" stay as before. The phone drawer no longer shows "Collapse menu".
- **No login flash**: the page opens with shimmer. With a saved login, the real top bar (☰, page name, ↻, bell, photo + crown) shows at once and the page area shimmers in the shape of the page you were on (Map, Dashboard, lists). The real data replaces it in place.
- **Only an expired login shows the login page.** Wi-Fi drop, server restarting (5xx) or no answer in 10 s: you stay in the panel, the shimmer stays, and an amber bar says **"Can't reach the server · Trying again in N s…"** with **Retry**. It retries by itself after 5, 10, 20, 30, 30 … s, and at once when you come back to the tab.
- **Too many refreshes (429)**: you stay signed in; the bar says **"Server is busy · Trying again in N s…"** (Q1 = A). The same bar replaces the old long "throttling" text everywhere.
- **Page switch** shows the same page-shaped shimmer (Q2 = A).
- Reduce motion on: grey blocks, no shine.

## Files
- `internal/httpapi/adminweb/index.html` — `data-theme="dark"` on `<html>` (the default, so the first shimmer frame is dark), static boot shimmer `#boot219`, login page starts hidden, ☰ no longer starts hidden.
- `internal/httpapi/adminweb/styles.css` — bundle 219 block at the end: menu button by width, boot shell, page shimmer shapes, amber bar.
- `internal/httpapi/adminweb/app.js` — `paintShell219`, `startView219`, `bootCheck219` (10 s abort, retry steps, 401 / 429 / 5xx / offline), `syncLayout219` (matchMedia 1000 px + pageshow), rate-limit bar text, page-shaped shimmer in `mountView`.
- `internal/httpapi/adminweb/components.js` — `ui.bootSkeleton219(viewId)`.
- `internal/httpapi/adminweb/views.js` — 8 one-line changes (see below).
- `internal/httpapi/admin_web_219_test.go` — new.
- `APPLY_219.md`, `TODO_219.md`.

## Different from TODO_219 (told in the chat)
1. **`views.js` added.** Dashboard, Users, Devices, Sessions, Security, Audit log, Recovery and Map each painted their own plain grey panel while loading, which would replace the page-shaped shimmer halfway. Each now calls `ui.bootSkeleton219('<id>')` (one line each, nothing else changed).
2. **429 correction.** The TODO said a 429 deletes the login. It does not (`noteRateLimit` keeps it); the real problem was only the boot catch sending you to the login page. Fixed there; the bar text changed to "Server is busy".
3. `<html data-theme="dark">` added so the very first shimmer frame is not white for the default dark theme (a light-theme user may see one dark frame before the script switches).
4. Any other unexpected answer at boot (not 401 / 429 / 5xx / offline) still goes to the login page, as before.

## Checks done here
- `go test ./...` all green; new `TestAdminWeb219RefreshShimmerAndMenu` passes; old web tests unchanged.
- jsdom: 7 scenarios (slow server, 401, offline + Retry, no login, 429, 503 auto retry, page switch + drawer) all pass; the 217 jsdom check still passes (phone and computer width).
- Real Chromium (headless) at 360×780 and 1280×760: ☰ shown on phone right after load, hidden on computer; "Collapse menu" only on computer; login page hidden; no sideways scroll. `ui_check_219.jpg` (in the chat) puts them next to the mocks.

## Phone tests
See TODO_219 §8 (219-1 … 219-8).
