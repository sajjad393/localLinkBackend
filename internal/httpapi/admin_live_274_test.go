package httpapi

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Bundle 274: GET /api/v1/admin/live (plain ws, first-frame auth).

type liveConn274 struct {
	c net.Conn
	r *bufio.Reader
}

func dialLive274(t *testing.T, srv *httptest.Server, query string) (*liveConn274, int) {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	req := "GET /api/v1/admin/live" + query + " HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	res, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &liveConn274{c: c, r: r}, res.StatusCode
}

func (l *liveConn274) send(t *testing.T, v any) {
	t.Helper()
	p, _ := json.Marshal(v)
	mask := [4]byte{1, 2, 3, 4}
	h := []byte{0x81}
	switch {
	case len(p) < 126:
		h = append(h, 0x80|byte(len(p)))
	default:
		h = append(h, 0x80|126, byte(len(p)>>8), byte(len(p)))
	}
	h = append(h, mask[:]...)
	for i := range p {
		p[i] ^= mask[i%4]
	}
	if _, err := l.c.Write(append(h, p...)); err != nil {
		t.Fatal(err)
	}
}

// next reads one server (unmasked) frame; pings are skipped.
func (l *liveConn274) next(wait time.Duration) (byte, []byte, error) {
	_ = l.c.SetReadDeadline(time.Now().Add(wait))
	for {
		var h [2]byte
		if _, err := io.ReadFull(l.r, h[:]); err != nil {
			return 0, nil, err
		}
		n := uint64(h[1] & 0x7F)
		switch n {
		case 126:
			var x uint16
			if err := binary.Read(l.r, binary.BigEndian, &x); err != nil {
				return 0, nil, err
			}
			n = uint64(x)
		case 127:
			if err := binary.Read(l.r, binary.BigEndian, &n); err != nil {
				return 0, nil, err
			}
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(l.r, p); err != nil {
			return 0, nil, err
		}
		if op := h[0] & 0x0F; op != 0x9 && op != 0xA {
			return op, p, nil
		}
	}
}

func (l *liveConn274) frame(t *testing.T, wait time.Duration) map[string]any {
	t.Helper()
	op, p, err := l.next(wait)
	if err != nil {
		t.Fatalf("no frame: %v", err)
	}
	if op != 0x1 {
		t.Fatalf("want a text frame, got op %d %q", op, p)
	}
	var m map[string]any
	if err := json.Unmarshal(p, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func liveServer274(t *testing.T) (*setup216, *httptest.Server) {
	t.Helper()
	s := newSetup216(t)
	srv := httptest.NewServer(http.HandlerFunc(s.api.AdminLive))
	t.Cleanup(srv.Close)
	return s, srv
}

func TestAdminLiveSendsMapAfterFirstFrameAuth274(t *testing.T) {
	s, srv := liveServer274(t)
	l, code := dialLive274(t, srv, "")
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade %d", code)
	}
	l.send(t, map[string]string{"type": "auth", "token": s.token})
	m := l.frame(t, 5*time.Second)
	if m["type"] != "link_map" {
		t.Fatalf("first frame = %v", m)
	}
	nodes, _ := m["nodes"].([]any)
	if len(nodes) < 2 {
		t.Fatalf("the map must list the phones, got %v", m["nodes"])
	}
	// A phone's links change -> a new frame within about a second.
	s.api.noteNeighbors216("dev-ali-216", []Neighbor{{"dev-bilal-216", "wifi_direct"}})
	start := time.Now()
	m = l.frame(t, 4*time.Second)
	if m["type"] != "link_map" {
		t.Fatalf("change frame = %v", m)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("change frame too slow: %v", time.Since(start))
	}
	edges, _ := m["edges"].([]any)
	found := false
	for _, e := range edges {
		em, _ := e.(map[string]any)
		if fmt.Sprint(em["link"]) == "wifi_direct" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the new Wi-Fi Direct line is missing: %v", edges)
	}
}

func TestAdminLiveRefusesBadToken274(t *testing.T) {
	_, srv := liveServer274(t)
	l, code := dialLive274(t, srv, "")
	if code != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade %d", code)
	}
	l.send(t, map[string]string{"type": "auth", "token": "not-a-token"})
	m := l.frame(t, 5*time.Second)
	if m["type"] != "error" {
		t.Fatalf("a bad token must get an error, got %v", m)
	}
	if op, _, err := l.next(3 * time.Second); err == nil && op != 0x8 {
		t.Fatalf("socket must close after a bad token (op %d)", op)
	}
}

func TestAdminLiveNeedsAuthFrameFirst274(t *testing.T) {
	s, srv := liveServer274(t)
	l, _ := dialLive274(t, srv, "")
	l.send(t, map[string]string{"type": "hello", "token": s.token})
	if m := l.frame(t, 5*time.Second); m["type"] != "error" {
		t.Fatalf("a non-auth first frame must be refused, got %v", m)
	}
}

func TestAdminLiveRefusesTokenInURL274(t *testing.T) {
	s, srv := liveServer274(t)
	_, code := dialLive274(t, srv, "?token="+s.token)
	if code != http.StatusForbidden {
		t.Fatalf("a token in the URL must get 403, got %d", code)
	}
}

func TestAdminLiveDebounceOneFramePerSecond274(t *testing.T) {
	s := newSetup216(t)
	sub := &liveSub274{adminID: "x", out: make(chan []byte, 1)}
	h := s.api.liveHub()
	if !h.add(sub) {
		t.Fatal("add")
	}
	defer h.remove(sub)
	for i := 0; i < 20; i++ {
		s.api.linkMapChanged274()
	}
	select {
	case <-sub.out:
	case <-time.After(2 * time.Second):
		t.Fatal("no frame after a burst of changes")
	}
	// The burst made exactly one frame; the next change waits for the 1 s gap.
	s.api.linkMapChanged274()
	start := time.Now()
	select {
	case <-sub.out:
		if d := time.Since(start); d < 600*time.Millisecond {
			t.Fatalf("second frame too soon: %v", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the second change never sent a frame")
	}
}

func TestAdminLiveLimitsViewsPerAdmin274(t *testing.T) {
	_, a := newTestAPI(t)
	h := a.liveHub()
	var subs []*liveSub274
	for i := 0; i < liveMaxPerAdmin; i++ {
		s := &liveSub274{adminID: "adm", out: make(chan []byte, 1)}
		if !h.add(s) {
			t.Fatalf("view %d refused", i)
		}
		subs = append(subs, s)
	}
	if h.add(&liveSub274{adminID: "adm", out: make(chan []byte, 1)}) {
		t.Fatal("more than 5 live views for one admin")
	}
	for _, s := range subs {
		h.remove(s)
	}
	if h.count() != 0 {
		t.Fatal("views not removed")
	}
}

func TestOfferLatestKeepsNewest274(t *testing.T) {
	ch := make(chan []byte, 1)
	offerLatest274(ch, []byte("a"))
	offerLatest274(ch, []byte("b"))
	if got := string(<-ch); got != "b" {
		t.Fatalf("slow reader must get the newest frame, got %q", got)
	}
}

func TestOnlineMeansConnectedNow274(t *testing.T) {
	_, a := newTestAPI(t)
	if !a.onlineNow274("p1", true) || a.onlineNow274("p1", false) {
		t.Fatal("no live state: fall back to the seen rule")
	}
	a.presenceMu.Lock()
	a.presence["p1"] = DevicePresence{ServerConnected: false}
	a.presenceMu.Unlock()
	if a.onlineNow274("p1", true) {
		t.Fatal("a phone that left the server is offline at once")
	}
}

func TestSameNeighbors274(t *testing.T) {
	x := []Neighbor{{"a", "mesh"}, {"b", "wifi_direct"}}
	if !sameNeighbors274(x, []Neighbor{{"b", "wifi_direct"}, {"a", "mesh"}}) {
		t.Fatal("order must not matter")
	}
	if sameNeighbors274(x, []Neighbor{{"a", "mesh"}, {"b", "lan"}}) || sameNeighbors274(x, x[:1]) {
		t.Fatal("a changed link must count as a change")
	}
}

func TestMapAction274(t *testing.T) {
	for _, a := range []string{"device_revoked", "user_suspended", "user_block", "new_owner_235", "device_removed"} {
		if !mapAction274(a) {
			t.Fatalf("%s changes the map", a)
		}
	}
	for _, a := range []string{"admin_login", "admin_login_denied", "settings_saved"} {
		if mapAction274(a) {
			t.Fatalf("%s must not push a map frame", a)
		}
	}
}
