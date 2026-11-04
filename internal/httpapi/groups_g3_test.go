package httpapi

import (
	"encoding/json"
	"testing"
)

// Bundle G3: a group made on a phone keeps its id on the server.

const g3ID = "6f1c2a9e-4b7d-4c1e-9a3f-2d5e8b7c1a04"

func TestG3CreateWithPhoneIDIsIdempotentForOwner(t *testing.T) {
	e := newG2Env(t, "g3-owner", "g3-ali", "g3-sara")
	body := map[string]any{"name": "Office", "member_ids": []string{"g3-ali"}, "id": g3ID}
	res := e.do(e.a.CreateGroup, "POST", "/api/v1/groups", body, "g3-owner")
	if res.Code != 201 {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var g Group
	_ = json.Unmarshal(res.Body.Bytes(), &g)
	if g.ID != g3ID {
		t.Fatalf("id = %q", g.ID)
	}
	again := e.do(e.a.CreateGroup, "POST", "/api/v1/groups", body, "g3-owner")
	if again.Code != 200 {
		t.Fatalf("again: %d %s", again.Code, again.Body.String())
	}
	other := e.do(e.a.CreateGroup, "POST", "/api/v1/groups", body, "g3-sara")
	if other.Code != 409 {
		t.Fatalf("other owner: %d %s", other.Code, other.Body.String())
	}
	if len(e.events(g3ID, "g3-owner")) != 1 {
		t.Fatalf("expected one create line")
	}
}

func TestG3BridgeSkipsUnknownPeopleAndCreateLine(t *testing.T) {
	e := newG2Env(t, "g3b-owner", "g3b-ali")
	body := map[string]any{"name": "Trip", "member_ids": []string{"g3b-ali", "never-online"}, "id": g3ID, "bridge": true}
	res := e.do(e.a.CreateGroup, "POST", "/api/v1/groups", body, "g3b-owner")
	if res.Code != 201 {
		t.Fatalf("bridge: %d %s", res.Code, res.Body.String())
	}
	roles := e.roles(g3ID, "g3b-owner")
	if roles["g3b-owner"] != "owner" || roles["g3b-ali"] != "member" || len(roles) != 2 {
		t.Fatalf("roles = %v", roles)
	}
	if n := len(e.events(g3ID, "g3b-owner")); n != 0 {
		t.Fatalf("bridge wrote %d lines", n)
	}
	plain := map[string]any{"name": "X", "member_ids": []string{"never-online"}}
	if r := e.do(e.a.CreateGroup, "POST", "/api/v1/groups", plain, "g3b-owner"); r.Code != 404 {
		t.Fatalf("unknown without bridge: %d", r.Code)
	}
	bad := map[string]any{"name": "X", "member_ids": []string{"g3b-ali"}, "id": "not-a-uuid"}
	if r := e.do(e.a.CreateGroup, "POST", "/api/v1/groups", bad, "g3b-owner"); r.Code != 400 {
		t.Fatalf("bad id: %d", r.Code)
	}
}
