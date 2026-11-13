# APPLY backend 300 - linkup.local fix (backend only, no app build)

Zip: `locallink_backend_300_2026-11-14.zip`, on top of GitHub `9476165` (backend 299).
Plan: `docs/history/TODO_300.md`.

## What changes
- **The server PC's own browser can open `linkup.local`.** Go's multicast listener switched "loopback" off, and on Windows
  that hid every question asked by the PC itself. It is now switched back on (`internal/mdns299/loop_300_*.go`).
- **A question from the server PC gets a multicast answer.** Windows' own DNS service shares port 5353 with the server, so a
  direct (unicast) answer could land on the server's own socket instead of reaching Windows.
- **Questions from phones are answered as in 299:** an Android one-shot question gets a direct answer, with id + question echoed.
- **Each network socket answers only questions from its own network.** Before, every socket answered every question.
- **New server window line**, at most once per device every 10 minutes:
  `MDNS_299 asked by 192.168.18.40 for linkup.local (legacy) -> answered 192.168.18.27 (unicast)`
  - A phone tries and **no line** appears -> its question never reached the server (firewall or router).
  - **A line** appears but the phone still fails -> the answer went out; look at the phone/router.

## Files (7)
- `internal/mdns299/mdns.go` (Start split into `reply300`, the ask log, and per-network addresses)
- new `internal/mdns299/loop_300_unix.go`, `loop_300_windows.go`, `loop_300_other.go`
- new `internal/mdns299/loop_300_test.go`
- `docs/history/TODO_300.md`, `docs/history/APPLY_backend_300.md` (git add -f)

## Tests (run here, Linux)
- `go vet ./...` OK; Windows + macOS cross-build: `GOOS=windows go vet ./internal/mdns299`,
  `GOOS=windows go build ./cmd/server`, `GOOS=darwin go vet ./internal/mdns299` all OK.
- gofmt clean (changed/new files).
- Full `go test ./...`: all packages ok (cmd/server, discovery, httpapi, mdns299, store, tlsauto, ws).
- New tests:
  - `TestReply300Routing` covers who gets the answer: this PC, a phone with QU, an Android one-shot, a stranger network,
    another name.
  - `TestAskLog300OncePerAsker` checks one line per device per 10 minutes.
  - `TestSameHostAskerGetsAnswer300` is a real multicast test on this machine (not skipped here). It **fails without the fix**
    (`no answer from the responder on the same machine: i/o timeout`) and passes with it.
- Live server binary here:
  - An Android-style one-shot question got a direct answer (id echoed, IP inside).
  - A PC-style question from port 5353 got a multicast answer.
  - The window showed `MDNS_299 on: linkup.local` and `MDNS_299 asked by ...`.
- **NOT checked here:** real Windows (Windows' own 5353 service), Windows Firewall, your router, real phones.

## After the push
1. Restart the server. The window shows `MDNS_299 answering linkup.local on N network(s)` + `MDNS_299 on: linkup.local`.
   - `N` may be 2-4 (Wi-Fi, Ethernet, virtual adapters). That is fine.
2. One time, in PowerShell **as Administrator** (skip if you already ran it):
   `New-NetFirewallRule -DisplayName "LinkUp short name (UDP 5353)" -Direction Inbound -Protocol UDP -LocalPort 5353 -Action Allow -Profile Any`
   `New-NetFirewallRule -DisplayName "LinkUp port 80" -Direction Inbound -Protocol TCP -LocalPort 80 -Action Allow -Profile Any`
3. Server PC Chrome: `linkup.local/download` opens. The window shows `MDNS_299 asked by 192.168.18.27 ...`.
4. Phone Chrome (Android 12+): `linkup.local/download` opens. The window shows `MDNS_299 asked by <phone IP> ...`.
   - No "asked by" line for the phone -> its question never arrived. Send me a photo of the window.
   - Chrome may remember the old "can't be reached" for about a minute: close the tab, or try again after 1 minute.
5. Android 10/11 phones: keep using `192.168.18.27/download` (they cannot do `.local`).
