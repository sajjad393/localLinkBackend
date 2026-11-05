package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func doJSONReq216(t *testing.T, method, path, id string, headers map[string]string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.SetPathValue("id", id)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func recorder216(h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	res := httptest.NewRecorder()
	h(res, req)
	return res
}

type linkMap216 struct {
	Nodes []linkNode216 `json:"nodes"`
	Edges []linkEdge216 `json:"edges"`
}

func (s *setup216) linkMap(t *testing.T) linkMap216 {
	t.Helper()
	res := doJSON(t, s.api.AdminLinkMap, http.MethodGet, "/api/v1/admin/link-map", nil, map[string]string{"Authorization": "Bearer " + s.token})
	if res.Code != http.StatusOK {
		t.Fatalf("link-map %d %s", res.Code, res.Body.String())
	}
	var out linkMap216
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNeighborsAreCleanedAndCapped216(t *testing.T) {
	_, api := newTestAPI(t)
	list := []Neighbor{{"me", "mesh"}, {"x", "bogus"}, {"a", "wifi_direct"}, {"a", "mesh"}, {"", "lan"}}
	for i := 0; i < 50; i++ {
		list = append(list, Neighbor{DeviceID: fmt.Sprintf("p%02d", i), Link: "MESH"})
	}
	api.noteNeighbors216("me", list)
	got := api.linkStore().reports["me"].list
	if len(got) != maxNeighbors216 {
		t.Fatalf("cap 32, got %d", len(got))
	}
	if got[0] != (Neighbor{"a", "wifi_direct"}) || got[1].Link != "mesh" {
		t.Fatalf("cleaned list = %v", got[:3])
	}
	api.noteNeighbors216("me", nil) // no field sent: keep the last report
	if len(api.linkStore().reports["me"].list) != maxNeighbors216 {
		t.Fatal("a presence frame without neighbours must not wipe the report")
	}
}

func TestLinkMapOneNodePerPhoneWithLines216(t *testing.T) {
	s := newSetup216(t)
	s.api.noteNeighbors216("dev-ali-216", []Neighbor{{"dev-bilal-216", "wifi_direct"}})
	s.api.noteNeighbors216("dev-bilal-216", []Neighbor{{"dev-ali-216", "wifi_direct"}, {"unknown-phone", "mesh"}})
	m := s.linkMap(t)
	count := map[string]int{}
	for _, n := range m.Nodes {
		count[n.DeviceID]++
	}
	for id, c := range count {
		if c != 1 {
			t.Fatalf("phone %s shown %d times", id, c)
		}
	}
	var ali *linkNode216
	for i := range m.Nodes {
		if m.Nodes[i].DeviceID == "dev-ali-216" {
			ali = &m.Nodes[i]
		}
	}
	if ali == nil || ali.DisplayName != "Ali Raza" || !ali.HasAvatar || ali.UserID != s.ali.Account.ID {
		t.Fatalf("ali node = %+v", ali)
	}
	direct := 0
	for _, e := range m.Edges {
		if e.Link == "wifi_direct" {
			direct++
		}
		if e.To == "unknown-phone" || e.From == "unknown-phone" {
			t.Fatal("unknown phones must not get a line")
		}
	}
	if direct != 1 {
		t.Fatalf("both phones report each other: want 1 line, got %d (%v)", direct, m.Edges)
	}
}

func TestLinkMapDropsOldReports216(t *testing.T) {
	s := newSetup216(t)
	s.api.noteNeighbors216("dev-ali-216", []Neighbor{{"dev-bilal-216", "mesh"}})
	st := s.api.linkStore()
	st.mu.Lock()
	r := st.reports["dev-ali-216"]
	r.at = time.Now().Add(-5 * time.Minute)
	st.reports["dev-ali-216"] = r
	st.mu.Unlock()
	for _, e := range s.linkMap(t).Edges {
		if e.Link == "mesh" {
			t.Fatal("reports older than 4 minutes must not draw lines")
		}
	}
}

func TestLinkMapNeedsAdmin216(t *testing.T) {
	s := newSetup216(t)
	if res := doJSON(t, s.api.AdminLinkMap, http.MethodGet, "/api/v1/admin/link-map", nil, directoryHeaders(s.ali)); res.Code == http.StatusOK {
		t.Fatal("normal users must not read the link map")
	}
}

func TestPresenceFrameCarriesNeighbors216(t *testing.T) {
	var msg WSMessage
	if err := json.Unmarshal([]byte(`{"type":"presence_update","neighbors":[{"device_id":"b","link":"mesh"}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	if len(msg.Neighbors) != 1 || msg.Neighbors[0].DeviceID != "b" {
		t.Fatalf("neighbors = %v", msg.Neighbors)
	}
	raw, _ := json.Marshal(WSMessage{Type: "presence_update"})
	if string(raw) != `{"type":"presence_update"}` {
		t.Fatalf("old frames must stay the same: %s", raw)
	}
}
