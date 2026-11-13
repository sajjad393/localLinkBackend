# APPLY backend 299 (LinkUp from the server + short address + faster admin panel)

Zip: `locallink_backend_299_2026-11-14.zip`, on top of GitHub `94966fc` (backend 297).
**Deploy this BEFORE app 299** (the app asks `/api/v1/app/latest`; old servers answer 404 and the app just uses nearby phones).
New table (migration 62 `app_apk_299`, made by itself at start). No new setting is needed.

## What changes
- **A. Web "App download" (Owner only)**, sidebar System > App download.
  - Drag the APK in, or Choose file. Progress bar, then "Checking the APK...".
  - Refused in plain words, the old APK stays: not an APK / not LinkUp / not newer than the current one /
    signed with a different key / too big (max 300 MB) / disk full.
  - A newer APK deletes the old file at once. History keeps every version (name, version, size, who, when, downloads).
  - Remove asks first. Copy link, Open page, QR of the link.
  - Other roles do not see it (server answers 403 too).
- **B. Public page** (no sign-in) for any phone on the server's Wi-Fi:
  `/download` (also `/Download`, `/linkup`, `/LinkUp`, with or without `/` at the end).
  Big Download button, 3 steps (English + Urdu), QR. The file link is `/download/LinkUp-<version>.apk`
  (resume works). Downloads are counted once per phone per 10 minutes.
- **C. For the app:** `GET /api/v1/app/latest` (signed-in phones) = version, build, size, sha256. Not blocked by the minimum
  version rule, so a stopped old phone can still get the new APK.
- **N. Short address, nothing to type extra:**
  - The server also listens on **port 80**, so `192.168.18.27/download` works without `:8080` (8080 keeps working; the apps keep 8080).
  - The server answers **`linkup.local`** on the Wi-Fi: `linkup.local` = admin panel, `linkup.local/download` = download page
    (Android 12+ phones, Windows 10/11 PCs). Android 10/11 phones: use the IP link or scan the QR.
  - `/` now opens the admin panel.
  - Server window lines: `PORT80_299 on` / `PORT80_299 off: <why>` and `MDNS_299 on: linkup.local` / `MDNS_299 off: <why>`.
    The App download page shows a yellow line when one of them failed (for example "port 80 off ... phones must type :8080").
  - Switch off if ever needed: `LOCALLINK_HTTP80=off`, `LOCALLINK_MDNS_NAME=off`.
- **S. Admin panel faster:** scripts/styles are sent gzipped (views.js 192 KB -> 52 KB) and checked with an ETag
  (the second open downloads almost nothing: 304). The sign-in "last seen" is written once a minute instead of on every call.
  The page itself still is never cached.

## Why the admin panel felt slow (answer)
The server answers each call in about 1-3 ms. The time went to (1) downloading about 383 KB of scripts on every open
(no gzip, no cache: now about 98 KB the first time, almost 0 after), and (2) a database write on every admin call
("last seen"), which waits for the one database line. Both are fixed here.

## Bare `linkup` (no `.local`), router steps (your router, once)
1. Router page (usually `192.168.18.1`), sign in.
2. LAN / DHCP: **reserve** `192.168.18.27` for the server PC (so the IP never changes).
3. If the router has "Static DNS" / "Host name" / "DNS records": add `linkup` -> `192.168.18.27`.
4. On a phone type `linkup/download` (with `/download`, else Chrome searches Google).
Tell me the router brand/model for exact clicks. Not all home routers have step 3; `linkup.local` works without it.

## Files
`cmd/server/main.go`, new `cmd/server/short_names_299.go` + test;
`internal/httpapi/`: new `apk299.go`, `apk_check_299.go`, `qr_299.go`, `linkup_page_299.go`, tests `apk299_test.go`,
`admin_speed_299_test.go`; changed `api.go`, `admin_web.go`, `admin_auth.go`, `version_gate.go`,
`adminweb/views.js`, `adminweb/styles.css`;
new `internal/mdns299/mdns.go` + test; `internal/store/store.go` (migration 62);
`docs/history/TODO_299.md`, `docs/history/APPLY_backend_299.md`. Old tests unchanged.

## Tests (here)
- `go test -count=1 ./...` all packages ok (exit 0), including `TestAdminWebOnlyCallsRegisteredEndpoints`; `go vet` OK;
  gofmt clean on every new/changed file; JS syntax OK.
- APK reader checked on 2 real APKs from F-Droid: package, versionCode, versionName right; key hash = the
  `META-INF/*.RSA` certificate hash (openssl).
- Live server here: upload ok, wrong app refused (422), newer replaces + deletes the old file, download byte-equal,
  page on all 4 paths, `/` -> admin, gzip 191879 -> 51592 bytes, 304 on the second load.
- Real admin page in Chromium: App download page, upload of an older APK shows the red "must be newer" box; 0 page errors.
- QR codes decoded by a real QR reader (zxing): 6/6.
- **Not tested:** Windows (port 80 / 5353 next to Windows' own services), real phones, a real 60-100 MB APK on Wi-Fi.
- Limitation: only APKs with a v2/v3 signature are accepted (every GitHub Flutter release APK has one).

## Commands
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
git pull
Expand-Archive -Force "$env:USERPROFILE\Downloads\locallink_backend_299_2026-11-14.zip" .
go build ./...
go test ./...
git add internal/httpapi internal/mdns299 internal/store cmd/server
git add -f docs/history/TODO_299.md docs/history/APPLY_backend_299.md
git commit -m "v3 backend 299: owner-only App download page (APK upload with checks, history, old file deleted), public /download page + file, /api/v1/app/latest for phones (version gate exempt), port 80 + linkup.local, admin panel gzip + ETag, last_seen once a minute"
git push
```
Restart the server. **Windows may ask once to allow LinkUp server on the network: click Allow (Private).**
Then hard refresh the admin page (Ctrl+F5).

## Check on the PC / phones
1. Server window shows `PORT80_299 on` and `MDNS_299 on: linkup.local`.
2. PC browser: `linkup.local` opens the admin panel. System > App download: drag the 3.0.96 APK (from app 299's GitHub build).
3. Phone (Android 12+): Chrome `linkup.local/download` -> Download -> install. Android 10/11: `192.168.18.27/download`.
4. Try a wrong file (any other APK): red box, the old APK stays.
