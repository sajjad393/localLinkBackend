# LocalLink Backend — Bundle 49a (conversation media UI + topology fix)

Apply by extracting this zip **over the backend repo root** (`LocalLinkBackend`),
so `cmd\` and `internal\` land on top of the existing folders.

## What changed

**Presence counters now tell the truth (the "0 server links" bug).**
`internal/httpapi/websocket.go` restamps device presence on *every* websocket
frame — including protocol ping/pong — via a new `noteDeviceFrame` helper in
`api.go`. Presence used to be stamped only at connect time, so every healthy
socket decayed out of the 35-second TTL window and the admin console showed 0
server links / offline devices while both phones were connected.
`devices.last_seen_at` gets a throttled bump (≤ 1 write / 30 s / device), so
the "online" count in Devices stays truthful too.

**Conversation console shows the same message UI for text, audio, files and images.**
- Thread JSON now carries `attachments[]` per message (direct + group).
- New owner-only endpoint `GET /api/v1/admin/conversation-media?file_id=…[&thumb=1]`
  streams bytes inline for audio/image/video, forces download disposition for
  other types, audits every view (`admin_conversation_media_viewed`), and
  rejects anything that is not a clean file id.
- `adminweb/views.js` renders chat bubbles cloned from the app's MessageBubble
  (neutral surfaces, 82% width, tightened sender corner, 12 h time, ✓/✓✓
  ticks; second participant on the right, group senders labelled on the left).
  Audio plays inline, images show thumbnails and zoom in a lightbox, other
  files download. Media is fetched with the bearer header and rendered from
  `blob:` object URLs (the console has no cookies, so plain element URLs would
  be refused). If the bytes cannot be fetched, the bubble honestly says
  "Not stored on this server" (mesh-only / pruned media).
- CSP in `admin_web.go` was extended with `img-src … blob:` and
  `media-src 'self' blob:` or the object URLs would be blocked.
- View-only: no send capability was added anywhere.

**Network topology popup redesigned.**
Devices now sit in two columns to the LEFT and RIGHT of the centred Server
node, each drawn as a clearly visible 16 px ring (3 px stroke, green when
online, grey when offline) with a centre dot, name + status label, joined to
the server by a solid green line when online and a dashed grey line when
offline. The broken `ui.hexToRgba ? LL.charts.hexToRgba(nodeColor, 0.35) : …`
expression was replaced by a real local `rgba()` helper.

## Files in this bundle (10)

```
cmd/server/main.go
internal/httpapi/api.go
internal/httpapi/websocket.go
internal/httpapi/admin_conversations.go
internal/httpapi/admin_conversations_test.go
internal/httpapi/admin_web.go
internal/httpapi/adminweb/app.js
internal/httpapi/adminweb/views.js
internal/httpapi/adminweb/styles.css
internal/httpapi/adminweb/index.html
```

## Verify after applying (PowerShell, run from the repo root)

```powershell
Get-ChildItem .\cmd\server\main.go, .\internal\httpapi\api.go, .\internal\httpapi\websocket.go, .\internal\httpapi\admin_conversations.go, .\internal\httpapi\admin_conversations_test.go, .\internal\httpapi\admin_web.go, .\internal\httpapi\adminweb\app.js, .\internal\httpapi\adminweb\views.js, .\internal\httpapi\adminweb\styles.css, .\internal\httpapi\adminweb\index.html | Select-Object Name, LastWriteTime

# Content markers — every line must find at least one match:
Select-String -Path .\cmd\server\main.go -Pattern "conversation-media"
Select-String -Path .\internal\httpapi\websocket.go -Pattern "noteDeviceFrame"
Select-String -Path .\internal\httpapi\api.go -Pattern "noteDeviceFrame"
Select-String -Path .\internal\httpapi\admin_web.go -Pattern "media-src 'self' blob:"
Select-String -Path .\internal\httpapi\adminweb\views.js -Pattern "bubbleThread"
Select-String -Path .\internal\httpapi\adminweb\app.js -Pattern "leftX"
Select-String -Path .\internal\httpapi\adminweb\styles.css -Pattern "chat-bubble"

# Build + tests (all must pass; the three new tests must appear with -v):
go build ./...
go test ./...
go test -v ./internal/httpapi/ -run "TestAdminConversation|TestAdminConversationMediaEndpoint|TestNoteDeviceFrame"
```

## Then restart the server and check

1. Open the admin console → sign in as **owner** → Conversations → open a
   thread that has media: audio plays inline, images show and zoom, files
   download. Network tab: requests go to `/api/v1/admin/conversation-media`.
2. Topology popup (dashboard): both phones appear as green rings left/right
   of the Server with solid lines, and the "server links" badge stays
   non-zero while the phones sit idle (it no longer decays to 0).

## Suggested commit message

```
admin: media-rich conversation console, truthful presence, fixed topology

Presence: restamp UpdatedAt on every websocket frame (setPresence only
stamped connect time, so healthy sockets decayed out of the 35s TTL and
the console showed 0 server links); throttle-bump devices.last_seen_at
(<=1 write/30s/device) so the online count survives WS-only foregrounding.

Conversations: thread endpoints now return attachments[] for direct and
group messages; new owner-only GET /api/v1/admin/conversation-media
streams bytes (inline for audio/image/video, RFC 5987 attachment
otherwise), validates file ids, and audits every view. Admin web renders
app-style chat bubbles (text, inline audio, image thumbnails with
lightbox, file download chips, honest "not stored on this server" cards),
fetching media with the bearer header into blob: object URLs; CSP gains
img-src/media-src blob: accordingly.

Topology: devices laid out in columns left/right of the centered server,
drawn as visible 3px-stroke rings (green online / grey offline) with
name+status labels, joined by solid/dashed lines; replaces the broken
ui.hexToRgba fallback with a real local rgba() helper.

Tests: attachments in thread JSON, media endpoint auth/validation/
disposition/audit, noteDeviceFrame presence+last_seen freshness.
```
