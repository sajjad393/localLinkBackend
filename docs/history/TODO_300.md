# TODO 300 - linkup.local does not open (backend only, no app build)

Status: BUILT (14 Nov) - zip locallink_backend_300_2026-11-14.zip. Point d) stays NOT planned.
Base: backend `9476165` (299).

## What the user saw (14 Nov)
- The server PC's Chrome shows `linkup.local/download` -> "This site can't be reached", DNS_PROBE_FINISHED_NXDOMAIN.
- A phone on the same Wi-Fi gets the same error.
- `http://192.168.18.27/download` (port 80) opens on the PC, so the server is running 299.
- The user saw no `MDNS_299` line in the server window. The code always prints one of:
  `MDNS_299 on` / `MDNS_299 off: ...` / `MDNS_299 skip ...`.

## Cause 1 (certain, code): the server PC can never resolve its own name
- `internal/mdns299/mdns.go:209` uses `net.ListenMulticastUDP`.
- The Go docs say it sets `IP_MULTICAST_LOOP = 0`. I checked Go 1.22 `net/udpsock.go:351`.
- On Windows that option works on the RECEIVE side (Winsock). So our socket never hears the questions that Windows' own
  DNS client on the same PC sends. Result: the server PC's browser always fails.
- On Linux the same option blocks our answer from coming back to a local asker. Same result.
- The 299 tests sent a one-shot (legacy) question straight to the socket, so they never covered this case.
- Fix:
  - a) After `ListenMulticastUDP`, set `IP_MULTICAST_LOOP = 1` again with `SyscallConn().Control`. Standard library only,
    two small build-tagged files: windows `syscall.Handle`, unix `int`.
    Our own answers then come back to us; `parseQuery` already ignores responses (`msg[2]&0x80`).
  - b) A question from this PC itself (src IP = one of our IPs) on port 5353: answer by multicast, not unicast.
    Windows' DNS client shares port 5353 with us (SO_REUSEADDR), and a unicast packet to that port may land on our own
    socket instead of Windows'. A legacy question (src port != 5353) still gets a unicast answer to its own port.

## Cause 2 (likely, not proven): phones' questions never reach the server
- Android 12+ sends a one-shot question from a random port to 224.0.0.251:5353 (AOSP DnsResolver).
- Our socket must receive it, so the Windows Firewall has to allow inbound UDP 5353 to the server program.
  - Same-PC traffic skips the firewall. That is why the PC test of port 80 says nothing about phones.
  - Phones reach 8080 today, but that may be a TCP-only or port-only rule.
- Other possible causes: the router blocks multicast between Wi-Fi devices, or the phone's resolver has mDNS off.
- Fix:
  - c) Log once per asker per 10 min: `MDNS_299 asked by <ip> for <name> (legacy|mdns) -> answered <ip>`.
    Seeing no line while a phone tries = the question never arrived (firewall/router). Seeing a line = the answer went
    out, so look at the phone.
  - d) The server window prints the firewall command on start, once, when no question has arrived within 2 min of
    `MDNS_299 on`? NOT planned (guessing). Docs only.
  - e) Docs: a one-time admin PowerShell firewall rule for UDP 5353 + TCP 80 (in the reply to the user, 14 Nov).

## Tests (new `internal/mdns299/loop_300_test.go`; old tests unchanged)
- Start the responder on a test name. A multicast question sent FROM THIS HOST from port 5353 gets an answer on a
  second socket bound to 5353 (loop on). Skip when the sandbox has no multicast interface (say so, do not fake).
- A same-host question with the QU bit set -> the answer is sent to the group, not unicast.
- A legacy question -> unicast to its port, id + question echoed (existing behaviour kept).
- The log helper prints once per asker within 10 min.
- Full `go test ./...`, vet, gofmt (new files only).

## Files
- `internal/mdns299/mdns.go`
- new `internal/mdns299/loop_300_unix.go`, `internal/mdns299/loop_300_windows.go`
- new `internal/mdns299/loop_300_test.go`
- `docs/history/TODO_300.md`, `docs/history/APPLY_backend_300.md`

## Not touched
- Port 80 code, App download page, app code.
- No health rows (`admin_overview_test.go:168` lock).

## Phone/PC checks after the build
1. Server window: `MDNS_299 on: linkup.local` and `MDNS_299 answering linkup.local on N network(s)`.
2. Server PC Chrome: `linkup.local/download` opens. Window shows `MDNS_299 asked by 192.168.18.27`.
3. Phone Chrome (Android 12+): `linkup.local/download` opens. Window shows `MDNS_299 asked by <phone ip>`.
   - No "asked by" line -> firewall/router; the phone question never arrived.
