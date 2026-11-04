package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// Bundle G6: invite link.

func (e *g2Env) invite(gid, who string, body map[string]any) (int, string) {
	e.t.Helper()
	res := e.do(e.a.GroupInvite, "POST", "/api/v1/groups/invite?group_id="+gid, body, who)
	var out struct {
		Token string `json:"token"`
		Link  string `json:"link"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if res.Code == 200 && out.Link != "linkup://join/"+out.Token {
		e.t.Fatalf("link = %q", out.Link)
	}
	return res.Code, out.Token
}

func (e *g2Env) joinStatus(token, who, method string) (int, string) {
	e.t.Helper()
	h := e.a.GroupJoinPreview
	if method == "POST" {
		h = e.a.GroupJoin
	}
	res := e.do(h, method, "/api/v1/groups/join?token="+token, nil, who)
	var out struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out.Status
}

func TestG6InviteLinkAdminsOnlySameTokenReset(t *testing.T) {
	e := newG2Env(t, "g6-owner", "g6-ali")
	g := e.create("g6-owner", "g6-ali")
	if code, _ := e.invite(g.ID, "g6-ali", nil); code != 403 {
		t.Fatalf("member made a link: %d", code)
	}
	code, t1 := e.invite(g.ID, "g6-owner", nil)
	if code != 200 || len(t1) != 16 {
		t.Fatalf("invite: %d %q", code, t1)
	}
	if _, t2 := e.invite(g.ID, "g6-owner", map[string]any{}); t2 != t1 {
		t.Fatalf("same link expected, got %q", t2)
	}
	_, t3 := e.invite(g.ID, "g6-owner", map[string]any{"reset": true})
	if t3 == t1 {
		t.Fatalf("reset kept the token")
	}
	if code, _ := e.joinStatus(t1, "g6-ali", "GET"); code != 404 {
		t.Fatalf("old link still works: %d", code)
	}
	if code, st := e.joinStatus(t3, "g6-ali", "GET"); code != 200 || st != "member" {
		t.Fatalf("preview member: %d %s", code, st)
	}
	if code, _ := e.joinStatus("bad!", "g6-ali", "GET"); code != 404 {
		t.Fatalf("bad token: %d", code)
	}
}

func TestG6JoinDirectWhenNoApproval(t *testing.T) {
	e := newG2Env(t, "g6-owner", "g6-ali", "g6-stranger")
	g := e.create("g6-owner", "g6-ali")
	_, tok := e.invite(g.ID, "g6-owner", nil)
	res := e.do(e.a.GroupJoinPreview, "GET", "/api/v1/groups/join?token="+tok, nil, "g6-stranger")
	var p groupJoinPreview
	_ = json.Unmarshal(res.Body.Bytes(), &p)
	if res.Code != 200 || p.Name != "Office" || p.Members != 2 || p.Status != "none" || p.Approve || len(p.MemberNames) != 2 {
		t.Fatalf("preview: %d %+v", res.Code, p)
	}
	if code, st := e.joinStatus(tok, "g6-stranger", "POST"); code != 200 || st != "joined" {
		t.Fatalf("join: %d %s", code, st)
	}
	if e.roles(g.ID, "g6-owner")["g6-stranger"] != "member" {
		t.Fatalf("not a member")
	}
	evs := e.events(g.ID, "g6-stranger")
	if len(evs) == 0 || evs[len(evs)-1].Kind != "link_join" || evs[len(evs)-1].By != "g6-stranger" {
		t.Fatalf("events: %+v", evs)
	}
	if _, st := e.joinStatus(tok, "g6-stranger", "POST"); st != "member" {
		t.Fatalf("second join: %s", st)
	}
}

func TestG6AskToJoinThenApproveOrDecline(t *testing.T) {
	e := newG2Env(t, "g6-owner", "g6-ali", "g6-bilal", "g6-sara")
	g := e.create("g6-owner", "g6-ali")
	_, tok := e.invite(g.ID, "g6-owner", map[string]any{"approve": true})
	ask := e.do(e.a.GroupJoin, "POST", "/api/v1/groups/join?token="+tok, map[string]any{"seal": "c2VhbA", "sign": "c2lnbg=="}, "g6-bilal")
	if ask.Code != 200 || !strings.Contains(ask.Body.String(), `"requested"`) {
		t.Fatalf("ask: %d %s", ask.Code, ask.Body.String())
	}
	if bad := e.do(e.a.GroupJoin, "POST", "/api/v1/groups/join?token="+tok, map[string]any{"seal": "<x>"}, "g6-bilal"); bad.Code != 400 {
		t.Fatalf("bad key accepted: %d", bad.Code)
	}
	_, _ = e.joinStatus(tok, "g6-sara", "POST")
	if _, st := e.joinStatus(tok, "g6-bilal", "GET"); st != "requested" {
		t.Fatalf("preview: %s", st)
	}
	if e.roles(g.ID, "g6-owner")["g6-bilal"] != "" {
		t.Fatalf("joined without approval")
	}
	if res := e.do(e.a.GroupJoinRequests, "GET", "/api/v1/groups/join-requests?group_id="+g.ID, nil, "g6-ali"); res.Code != 403 {
		t.Fatalf("member saw requests: %d", res.Code)
	}
	res := e.do(e.a.GroupJoinRequests, "GET", "/api/v1/groups/join-requests?group_id="+g.ID, nil, "g6-owner")
	var out struct {
		Requests []groupJoinRequest `json:"requests"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if len(out.Requests) != 2 || out.Requests[0].DeviceID != "g6-bilal" || out.Requests[0].Seal != "c2VhbA" || out.Requests[0].Sign != "c2lnbg==" {
		t.Fatalf("requests: %+v", out.Requests)
	}
	// approve = normal add, even when not a friend
	add := e.do(e.a.AddGroupMember, "POST", "/api/v1/groups/members?group_id="+g.ID, map[string]any{"device_id": "g6-bilal"}, "g6-owner")
	if add.Code != 201 {
		t.Fatalf("approve: %d %s", add.Code, add.Body.String())
	}
	// decline is silent
	if d := e.do(e.a.GroupJoinDecline, "DELETE", "/api/v1/groups/join-requests?group_id="+g.ID+"&device_id=g6-sara", nil, "g6-owner"); d.Code != 200 {
		t.Fatalf("decline: %d", d.Code)
	}
	res = e.do(e.a.GroupJoinRequests, "GET", "/api/v1/groups/join-requests?group_id="+g.ID, nil, "g6-owner")
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if len(out.Requests) != 0 {
		t.Fatalf("left: %+v", out.Requests)
	}
	if _, st := e.joinStatus(tok, "g6-sara", "GET"); st != "none" {
		t.Fatalf("sara: %s", st)
	}
}

func TestG6JoinPage(t *testing.T) {
	e := newG2Env(t, "g6-owner")
	req := httptest.NewRequest("GET", "/join/Abc23456Defg7899", nil)
	req.SetPathValue("token", "Abc23456Defg7899")
	res := httptest.NewRecorder()
	e.a.GroupJoinPage(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "intent://join/Abc23456Defg7899#Intent;scheme=linkup;package=com.sajjad.linkup;end") {
		t.Fatalf("page: %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest("GET", "/join/x", nil)
	req.SetPathValue("token", "<script>")
	res = httptest.NewRecorder()
	e.a.GroupJoinPage(res, req)
	if res.Code != 404 {
		t.Fatalf("bad token page: %d", res.Code)
	}
}
