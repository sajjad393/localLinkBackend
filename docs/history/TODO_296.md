# TODO 296 BUILT (app 3.0.94+165 on 0f818ae, backend 296 on 9be3909)

Build go: "Go ahead H" + "Push 294 + 295 first, then build H (my advice)".

User answers 13 Nov: H = own build 296 after 295; far-friend checks every 10 s ONLY while LinkUp is open on screen.
App + backend (backend zip first).

# Part H - every map shows only true, live data (design only, freeze ON)

User rule (13 Nov): "Don't show wrong Data on any map showing real-time correct data on map everytime."

## Today: where wrong data comes from (code-grounded, app b5744ef / backend da74731)
| Map | Source | How old it can be |
|---|---|---|
| App "My links" card + chat dots | PresenceHub (my_links_map_273.dart:23) = "a stored path exists" (+20 s grace, presence_hub.dart:129) | 35-110 s after a way breaks (15 s heartbeat + 20 s grace; learned routes live 90 s, MeshRouteDelta.kt:35) |
| Web admin map + in-app admin map | Phones send neighbours every 30 s (presence_service.dart) - includes peers last seen up to **4 min** ago (presenceNeighbors216 `fourMin`) | up to **4 min** |
| same | Server keeps each report **4 min** (admin_216.go:427 neighborTTL216), also from phones that already left the server | up to 4 min more |
| same | Web refresh every 30 s (admin_live_274.go:29) + on change; an old report running out sends no update | +30 s |

## The rule after H
A line / colour on a map = **proven in the last 15 s**. No proof -> grey at once. (No radio can know a link died with 0 delay: it is known when an answer is missing. 15 s = 3 missed answers, so one lost packet never makes it flicker.)

| Map | After H |
|---|---|
| App card + chat dots | wrong for at most ~15 s (today 35-110 s); at call time 6 s (G1) |
| Web / in-app admin map | wrong for at most ~20 s (today up to 4-8 min) |

## H1 - proof for a direct link (native, display only)
- Every link already sends ping every 5 s and gets pong (LocalLinkTransportService.kt ~4706, 2229-2240).
- New flag per link: `proven` = our own pong came back in the last 10 s (or file data coming in now, Bundle 144 case).
- Maps use `proven`. The rule that closes links (15 s) does NOT change -> mesh / voice untouched.

## H2 - proof for a friend far away (through other phones)
- While LinkUp is open on screen: every 10 s a tiny locked "are you there" goes to each friend shown through mesh; the friend's phone answers at once, with the way it took.
- Friend in colour only if an answer (or anything else from them) came in the last 15 s. The "via" phone on the map = the real way the answer took, not a guess from the table.
- App in background: no extra packets; the normal 30 s beacon with a 45 s limit.
- Size: ~300 bytes per friend per 10 s (10 friends = ~0.3 KB/s). Friends only (rule: keep paths warm for friends only).
- Old app without H2: no answer -> keep today's rule for that phone (no false grey).

## H3 - app map rules (PresenceHub + My links card)
- Remove the 20 s grace (the 15 s proof window does that job).
- Lost proof -> grey at once, line removed. Back in colour only with new proof.
- G1 (calls) uses the same mark.

## H4 - web + in-app admin map (backend + app)
- App: send only proven links (H1/H2), not "seen in 4 min"; send at once when a link changes (2 s wait to group changes) and every 15 s while on the server.
- Server: a line counts only if
  - reported in the last 45 s (was 4 min), and
  - by a phone that is on the server now (a phone that left can't vouch for anything).
- Server checks every 5 s and pushes the change to open maps at once (today an old report running out is silent).
- 294 layout (colour rows for Wi-Fi Direct / mesh phones) stays.

## Not changed
- 15 s link close rule, 8 hops, mesh voice, route timers, groups.

## Files
- App: LocalLinkTransportService.kt (proven flag, probe answer), presence_hub.dart, presence_service.dart (presenceNeighbors216), my_links_map_273.dart (only if needed), a small reach-check service (Dart, uses the existing sealed send), tests.
- Backend: admin_216.go (45 s + online reporter), admin_live_274.go (5 s sweep push), tests (link_map_test.go etc. updated only where the 4 min rule is locked).

## Phone test
- 3 phones A-B-C. Turn B's Wi-Fi off: A's card and the web map grey C within ~15 s / ~20 s. B back: colour after the first answer.
- Walk away with a phone: line goes within ~15 s.
- Leave all still 10 min: no flicker.
