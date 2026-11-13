package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Bundle 296 (H4): the admin map shows only live links.

func ageReport296(s *setup216, id string, d time.Duration) {
	st := s.api.linkStore()
	st.mu.Lock()
	r := st.reports[id]
	r.at = time.Now().Add(-d)
	st.reports[id] = r
	st.mu.Unlock()
}

func hasEdge296(m linkMap216, a, b string) bool {
	for _, e := range m.Edges {
		if (e.From == a && e.To == b) || (e.From == b && e.To == a) {
			return true
		}
	}
	return false
}

func TestLinkReportCounts45Seconds296(t *testing.T) {
	if neighborTTL216 != 45*time.Second {
		t.Fatalf("report life = %v, want 45s", neighborTTL216)
	}
	s := newSetup216(t)
	s.api.noteNeighbors216("dev-ali-216", []Neighbor{{"dev-bilal-216", "mesh"}})
	ageReport296(s, "dev-ali-216", 40*time.Second)
	if !hasEdge296(s.linkMap(t), "dev-ali-216", "dev-bilal-216") {
		t.Fatal("a 40 s old report still counts")
	}
	ageReport296(s, "dev-ali-216", 50*time.Second)
	if hasEdge296(s.linkMap(t), "dev-ali-216", "dev-bilal-216") {
		t.Fatal("a 50 s old report must not draw a line")
	}
}

func TestLinkReportOnlyFromPhoneOnlineNow296(t *testing.T) {
	s := newSetup216(t)
	s.api.noteNeighbors216("dev-ali-216", []Neighbor{{"dev-bilal-216", "wifi_direct"}})
	if !hasEdge296(s.linkMap(t), "dev-ali-216", "dev-bilal-216") {
		t.Fatal("an online phone's fresh report draws its line")
	}
	s.api.presenceMu.Lock()
	s.api.presence["dev-ali-216"] = DevicePresence{ServerConnected: false}
	s.api.presenceMu.Unlock()
	if hasEdge296(s.linkMap(t), "dev-ali-216", "dev-bilal-216") {
		t.Fatal("a phone that left the server cannot vouch for its links")
	}
}

func TestLinkReportFrameEmptyClears296(t *testing.T) {
	var msg WSMessage
	if err := json.Unmarshal([]byte(`{"type":"link_report_296","neighbors":[]}`), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != linkReportType296 || msg.Neighbors == nil {
		t.Fatalf("an empty list must arrive as an empty list: %#v", msg.Neighbors)
	}
	s := newSetup216(t)
	s.api.noteLinkReport296("dev-ali-216", []Neighbor{{"dev-bilal-216", "mesh"}})
	s.api.noteLinkReport296("dev-ali-216", nil) // no field: keep
	if !hasEdge296(s.linkMap(t), "dev-ali-216", "dev-bilal-216") {
		t.Fatal("a frame without the field keeps the last report")
	}
	s.api.noteLinkReport296("dev-ali-216", msg.Neighbors) // []: no links now
	if hasEdge296(s.linkMap(t), "dev-ali-216", "dev-bilal-216") {
		t.Fatal("an empty report removes the line at once")
	}
}

func TestLinkMapSignature296(t *testing.T) {
	n := []linkNode216{{DeviceID: "a", Online: true}, {DeviceID: "b", Link: "mesh"}}
	e := []linkEdge216{{From: "server", To: "a", Link: "server"}, {From: "a", To: "b", Link: "mesh"}}
	sig := linkMapSig296(n, e)
	flip := []linkEdge216{e[1], {From: "a", To: "server", Link: "server"}}
	if linkMapSig296([]linkNode216{n[1], n[0]}, flip) != sig {
		t.Fatal("order and line direction must not matter")
	}
	if linkMapSig296(n, e[:1]) == sig {
		t.Fatal("a line gone is a change")
	}
	n2 := []linkNode216{{DeviceID: "a", Online: true}, {DeviceID: "b", Link: "wifi_direct"}}
	if linkMapSig296(n2, e) == sig {
		t.Fatal("a changed way is a change")
	}
	n3 := []linkNode216{{DeviceID: "a"}, {DeviceID: "b", Link: "mesh"}}
	if linkMapSig296(n3, e) == sig {
		t.Fatal("online -> offline is a change")
	}
}

func TestSweepPushesOnlyOnChange296(t *testing.T) {
	s := newSetup216(t)
	sig, push := s.api.sweepTick296("", 1)
	if !push || sig == "" {
		t.Fatal("first check pushes")
	}
	if _, push = s.api.sweepTick296(sig, 2); push {
		t.Fatal("same map: no frame")
	}
	if _, push = s.api.sweepTick296(sig, liveSweepForced296); !push {
		t.Fatal("every 30 s a frame anyway")
	}
	s.api.noteNeighbors216("dev-ali-216", []Neighbor{{"dev-bilal-216", "mesh"}})
	sig2, push := s.api.sweepTick296(sig, 3)
	if !push || sig2 == sig {
		t.Fatal("a new line pushes")
	}
	ageReport296(s, "dev-ali-216", time.Minute)
	if _, push = s.api.sweepTick296(sig2, 4); !push {
		t.Fatal("a report running out pushes within one 5 s check")
	}
}

func TestLiveLoopUsesSweep296(t *testing.T) {
	if liveSweep296 != 5*time.Second || liveSweepForced296*liveSweep296 != 30*time.Second {
		t.Fatal("check every 5 s, frame anyway every 30 s")
	}
	src, err := os.ReadFile("admin_live_274.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "time.NewTicker(liveSweep296)") || !strings.Contains(string(src), "a.sweepTick296(last, tick)") {
		t.Fatal("the live loop must use the 5 s sweep")
	}
	ws, err := os.ReadFile("websocket.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ws), "case linkReportType296:") || !strings.Contains(string(ws), "a.allowLinkReport296(deviceID)") {
		t.Fatal("link_report_296 frames are read and rate limited")
	}
}
