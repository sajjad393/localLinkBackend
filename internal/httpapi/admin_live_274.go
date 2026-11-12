package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"locallink/backend/internal/ws"
)

// Bundle 274: the admin map in real time.
//
// GET /api/v1/admin/live is a plain WebSocket (keep http). A browser can not
// send an Authorization header on a WebSocket and tokens never go in the URL,
// so the FIRST frame must be {"type":"auth","token":"<admin token>"} within
// 5 s (same admin "read" rule as GET /admin/link-map). The server then sends
// {"type":"link_map", nodes, edges, server, at} at once and again after every
// change (phone connects / leaves, a phone's links change, an admin action),
// at most once per second. Phones of active admins get a tiny
// {"type":"admin_link_map_changed"} hint on their normal socket.

const (
	liveAuthWait274   = 5 * time.Second
	liveGap274        = time.Second
	liveFirstDelay274 = 200 * time.Millisecond
	livePing274       = 25 * time.Second
	liveRefresh274    = 30 * time.Second
	liveMaxPerAdmin   = 5
)

type liveSub274 struct {
	adminID string
	out     chan []byte // newest frame only
}

type liveHub274 struct {
	mu       sync.Mutex
	subs     map[*liveSub274]struct{}
	perAdmin map[string]int
	timer    *time.Timer
	lastSent time.Time
	ticking  bool
}

var liveHubs274 sync.Map

func (a *API) liveHub() *liveHub274 {
	if h, ok := liveHubs274.Load(a); ok {
		return h.(*liveHub274)
	}
	h, _ := liveHubs274.LoadOrStore(a, &liveHub274{subs: map[*liveSub274]struct{}{}, perAdmin: map[string]int{}})
	return h.(*liveHub274)
}

// linkMapChanged274 collects changes; one frame per second at most.
func (a *API) linkMapChanged274() {
	if a == nil {
		return
	}
	h := a.liveHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.timer != nil {
		return
	}
	wait := liveGap274 - time.Since(h.lastSent)
	if wait < liveFirstDelay274 {
		wait = liveFirstDelay274
	}
	h.timer = time.AfterFunc(wait, a.flushLive274)
}

func (a *API) flushLive274() {
	h := a.liveHub()
	h.mu.Lock()
	h.timer = nil
	h.lastSent = time.Now()
	subs := make([]*liveSub274, 0, len(h.subs))
	for s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	if len(subs) > 0 {
		if frame, err := a.linkMapFrame274(); err == nil {
			for _, s := range subs {
				offerLatest274(s.out, frame)
			}
		}
	}
	a.hintAdminPhones274()
}

// offerLatest274 keeps only the newest frame for a slow reader.
func offerLatest274(ch chan []byte, frame []byte) {
	select {
	case ch <- frame:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- frame:
	default:
	}
}

func (a *API) linkMapFrame274() ([]byte, error) {
	now := time.Now()
	nodes, edges, err := a.linkMap216(now)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"type": "link_map", "nodes": nodes, "edges": edges, "server": "server",
		"at": now.UTC().Format(time.RFC3339Nano)})
}

// hintAdminPhones274: phones signed in to an active admin account and
// connected now get the hint (normal users get nothing).
func (a *API) hintAdminPhones274() {
	if a.hub == nil || a.store == nil {
		return
	}
	rows, err := a.store.DB().Query(`SELECT d.id FROM devices d JOIN admin_accounts aa ON aa.user_id=d.user_id AND aa.status='active'
		WHERE COALESCE(d.status,'active')='active' AND COALESCE(d.revoked_at,'')=''`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return
	}
	payload := mustJSON(WSMessage{Type: "admin_link_map_changed", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	for _, id := range ids {
		if a.hub.HasDevice(id) {
			a.hub.TrySendTo(id, payload)
		}
	}
}

func (h *liveHub274) add(s *liveSub274) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.perAdmin[s.adminID] >= liveMaxPerAdmin {
		return false
	}
	h.perAdmin[s.adminID]++
	h.subs[s] = struct{}{}
	return true
}

func (h *liveHub274) remove(s *liveSub274) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; !ok {
		return
	}
	delete(h.subs, s)
	h.perAdmin[s.adminID]--
	if h.perAdmin[s.adminID] <= 0 {
		delete(h.perAdmin, s.adminID)
	}
}

func (h *liveHub274) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// refreshLoop274: while someone watches, a frame every 30 s too (neighbour
// reports that age out and the 2 min "seen" rule change without an event).
func (a *API) refreshLoop274() {
	h := a.liveHub()
	h.mu.Lock()
	if h.ticking {
		h.mu.Unlock()
		return
	}
	h.ticking = true
	h.mu.Unlock()
	go func() {
		t := time.NewTicker(liveRefresh274)
		defer t.Stop()
		for range t.C {
			if h.count() == 0 {
				h.mu.Lock()
				h.ticking = false
				h.mu.Unlock()
				return
			}
			a.linkMapChanged274()
		}
	}()
}

// liveAdmin274 checks the token from the first frame with the normal rule.
func (a *API) liveAdmin274(token string) (AdminAccount, bool) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 512 {
		return AdminAccount{}, false
	}
	r, _ := http.NewRequest(http.MethodGet, "/api/v1/admin/live", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	admin, ok := a.authorizeAdminSession(r)
	if !ok || !adminPermission(admin.Role, "read") {
		return AdminAccount{}, false
	}
	return admin, true
}

// AdminLive: GET /api/v1/admin/live (WebSocket, first-frame auth).
func (a *API) AdminLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "GET is required")
		return
	}
	if !headerHasToken(r.Header.Values("Upgrade"), "websocket") || !headerHasToken(r.Header.Values("Connection"), "upgrade") || strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		writeError(w, http.StatusBadRequest, "invalid websocket upgrade headers")
		return
	}
	q := r.URL.Query()
	if q.Get("token") != "" || q.Get("access_token") != "" || q.Get("auth") != "" {
		writeError(w, http.StatusForbidden, "websocket credentials must not be sent in query parameters")
		return
	}
	if !a.allowSecurityAction("admin-live-ip", registrationKey(r), maxAdminAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "admin rate limit exceeded")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if !validWebSocketKey(key) {
		writeError(w, http.StatusBadRequest, "websocket upgrade required")
		return
	}
	setSecurityHeaders(w)
	hj, ok := w.(http.Hijacker)
	if !ok {
		writeError(w, http.StatusInternalServerError, "websocket unsupported")
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	if err := ws.Handshake(conn, key); err != nil {
		_ = conn.Close()
		return
	}
	client := ws.NewClient("admin-live", conn)
	defer client.Close()

	// First frame = auth, within 5 s.
	_ = client.SetDeadline(time.Now().Add(liveAuthWait274))
	opcode, payload, err := ws.ReadFrame(rw.Reader)
	if err != nil || opcode != ws.TextFrame {
		return
	}
	var first struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if json.Unmarshal(payload, &first) != nil || first.Type != "auth" {
		_ = client.WriteText(mustJSON(map[string]string{"type": "error", "error": "auth frame required"}))
		_ = client.WriteClose()
		return
	}
	admin, ok := a.liveAdmin274(first.Token)
	if !ok {
		_ = client.WriteText(mustJSON(map[string]string{"type": "error", "error": "invalid admin credentials"}))
		_ = client.WriteClose()
		return
	}
	sub := &liveSub274{adminID: admin.ID, out: make(chan []byte, 1)}
	h := a.liveHub()
	if !h.add(sub) {
		_ = client.WriteText(mustJSON(map[string]string{"type": "error", "error": "too many live views"}))
		_ = client.WriteClose()
		return
	}
	defer h.remove(sub)
	a.refreshLoop274()
	if frame, err := a.linkMapFrame274(); err == nil {
		if client.WriteText(frame) != nil {
			return
		}
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		ping := time.NewTicker(livePing274)
		defer ping.Stop()
		for {
			select {
			case frame := <-sub.out:
				if client.WriteText(frame) != nil {
					client.Close()
					return
				}
			case <-ping.C:
				if client.WritePing(nil) != nil {
					client.Close()
					return
				}
			case <-done:
				return
			}
		}
	}()
	for {
		_ = client.SetDeadline(time.Now().Add(90 * time.Second))
		opcode, payload, err := ws.ReadFrame(rw.Reader)
		if err != nil {
			return
		}
		switch opcode {
		case ws.PingFrame:
			_ = client.WritePong(payload)
		case ws.CloseFrame:
			_ = client.WriteClose()
			return
		}
		// Text frames after auth are ignored (the view is read only).
	}
}
