package httpapi

import (
	"encoding/json"
	"testing"
	"time"
)

// Bundle G5: receipts per member + typing.

func TestG5ReceiptsKeptAndListedForWriter(t *testing.T) {
	e := newG2Env(t, "g5-owner", "g5-ali", "g5-sara")
	g := e.create("g5-owner", "g5-ali", "g5-sara")
	m, _, err := e.a.insertGroupMessage("g5-owner", g.ID, "Lunch?", time.Now().UTC().Format(time.RFC3339Nano), "m-g5-1", nil)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := e.a.handleGroupReceipt("g5-ali", WSMessage{GroupID: g.ID, Status: "delivered", MessageIDs: []string{m.ID}}); err != nil {
		t.Fatalf("ali delivered: %v", err)
	}
	if err := e.a.handleGroupReceipt("g5-sara", WSMessage{GroupID: g.ID, Status: "read", MessageIDs: []string{m.ID}}); err != nil {
		t.Fatalf("sara read: %v", err)
	}
	// the writer's own receipt is ignored; a stranger is refused
	_ = e.a.handleGroupReceipt("g5-owner", WSMessage{GroupID: g.ID, Status: "read", MessageIDs: []string{m.ID}})
	if err := e.a.handleGroupReceipt("nobody", WSMessage{GroupID: g.ID, Status: "read", MessageIDs: []string{m.ID}}); err == nil {
		t.Fatalf("non-member accepted")
	}
	if err := e.a.handleGroupReceipt("g5-ali", WSMessage{GroupID: g.ID, Status: "seen", MessageIDs: []string{m.ID}}); err == nil {
		t.Fatalf("bad status accepted")
	}
	res := e.do(e.a.GroupReceipts, "GET", "/api/v1/groups/receipts?group_id="+g.ID, nil, "g5-owner")
	if res.Code != 200 {
		t.Fatalf("list: %d %s", res.Code, res.Body.String())
	}
	var out struct {
		Receipts []GroupReceipt `json:"receipts"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if len(out.Receipts) != 2 {
		t.Fatalf("receipts = %+v", out.Receipts)
	}
	byWho := map[string]GroupReceipt{}
	for _, r := range out.Receipts {
		byWho[r.DeviceID] = r
	}
	if byWho["g5-ali"].DeliveredAt == "" || byWho["g5-ali"].ReadAt != "" {
		t.Fatalf("ali = %+v", byWho["g5-ali"])
	}
	if byWho["g5-sara"].DeliveredAt == "" || byWho["g5-sara"].ReadAt == "" {
		t.Fatalf("sara = %+v", byWho["g5-sara"])
	}
	// read later keeps the first delivered time
	first := byWho["g5-ali"].DeliveredAt
	since := byWho["g5-sara"].UpdatedAt
	time.Sleep(5 * time.Millisecond)
	_ = e.a.handleGroupReceipt("g5-ali", WSMessage{GroupID: g.ID, Status: "read", MessageIDs: []string{m.ID}})
	res = e.do(e.a.GroupReceipts, "GET", "/api/v1/groups/receipts?group_id="+g.ID+"&since="+since, nil, "g5-owner")
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if len(out.Receipts) != 1 || out.Receipts[0].DeliveredAt != first || out.Receipts[0].ReadAt == "" {
		t.Fatalf("after read = %+v", out.Receipts)
	}
	// Ali sees nothing (not his messages); a non-member is refused
	res = e.do(e.a.GroupReceipts, "GET", "/api/v1/groups/receipts?group_id="+g.ID, nil, "g5-ali")
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if len(out.Receipts) != 0 {
		t.Fatalf("ali sees %+v", out.Receipts)
	}
	if r := e.do(e.a.GroupReceipts, "GET", "/api/v1/groups/receipts?group_id="+g.ID, nil, "nobody"); r.Code != 403 && r.Code != 401 {
		t.Fatalf("stranger: %d", r.Code)
	}
}

func TestG5TypingRateLimitAndMembersOnly(t *testing.T) {
	e := newG2Env(t, "g5t-owner", "g5t-ali")
	g := e.create("g5t-owner", "g5t-ali")
	on := true
	e.a.handleGroupTyping("g5t-owner", WSMessage{GroupID: g.ID, Enabled: &on})
	groupTypingMu.Lock()
	_, kept := groupTypingLast["g5t-owner|"+g.ID]
	groupTypingMu.Unlock()
	if !kept {
		t.Fatalf("typing not noted")
	}
	e.a.handleGroupTyping("stranger", WSMessage{GroupID: g.ID, Enabled: &on})
	groupTypingMu.Lock()
	_, stranger := groupTypingLast["stranger|"+g.ID]
	groupTypingMu.Unlock()
	if stranger {
		t.Fatalf("stranger typing passed")
	}
	off := false
	e.a.handleGroupTyping("g5t-owner", WSMessage{GroupID: g.ID, Enabled: &off})
	groupTypingMu.Lock()
	_, still := groupTypingLast["g5t-owner|"+g.ID]
	groupTypingMu.Unlock()
	if still {
		t.Fatalf("typing off kept")
	}
}
