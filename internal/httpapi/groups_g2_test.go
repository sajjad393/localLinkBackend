package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Bundle G2: admins, settings, info, photo, leave and system lines.

type g2Env struct {
	t   *testing.T
	a   *API
	hdr map[string]map[string]string
}

func newG2Env(t *testing.T, ids ...string) *g2Env {
	_, a := newTestAPI(t)
	e := &g2Env{t: t, a: a, hdr: map[string]map[string]string{}}
	for _, id := range ids {
		tok := registerAuditDevice(t, a, id)
		e.hdr[id] = map[string]string{"Authorization": "Bearer " + tok, "X-Device-ID": id, "Content-Type": "application/json"}
	}
	return e
}

func (e *g2Env) do(h http.HandlerFunc, method, path string, body any, who string) *httptest.ResponseRecorder {
	e.t.Helper()
	return doJSON(e.t, h, method, path, body, e.hdr[who])
}

func (e *g2Env) raw(h http.HandlerFunc, method, path string, body []byte, who string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range e.hdr[who] {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "image/jpeg")
	res := httptest.NewRecorder()
	h(res, req)
	return res
}

func (e *g2Env) create(owner string, members ...string) Group {
	e.t.Helper()
	res := e.do(e.a.CreateGroup, "POST", "/api/v1/groups", map[string]any{"name": "Office", "member_ids": members}, owner)
	if res.Code != 201 {
		e.t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var g Group
	_ = json.Unmarshal(res.Body.Bytes(), &g)
	return g
}

func (e *g2Env) roles(gid, who string) map[string]string {
	e.t.Helper()
	res := e.do(e.a.GroupMembers, "GET", "/api/v1/groups/members?group_id="+gid, nil, who)
	if res.Code != 200 {
		e.t.Fatalf("members: %d %s", res.Code, res.Body.String())
	}
	var list []GroupMember
	_ = json.Unmarshal(res.Body.Bytes(), &list)
	out := map[string]string{}
	for _, m := range list {
		out[m.DeviceID] = m.Role
	}
	return out
}

func (e *g2Env) events(gid, who string) []groupEvent {
	e.t.Helper()
	res := e.do(e.a.ListGroupMessages, "GET", "/api/v1/group-messages?group_id="+gid, nil, who)
	if res.Code != 200 {
		e.t.Fatalf("messages: %d %s", res.Code, res.Body.String())
	}
	var page struct {
		GroupMessages []GroupMessage `json:"group_messages"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &page)
	var out []groupEvent
	for _, m := range page.GroupMessages {
		if !strings.HasPrefix(m.Body, groupEventPrefix) {
			continue
		}
		var ev groupEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(m.Body, groupEventPrefix)), &ev); err != nil {
			e.t.Fatalf("bad event %q", m.Body)
		}
		out = append(out, ev)
	}
	return out
}

func errorCode(res *httptest.ResponseRecorder) string {
	var body struct {
		Code string `json:"error_code"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	return body.Code
}

func TestG2CreateDefaultsAndCreateLine(t *testing.T) {
	e := newG2Env(t, "a", "b", "c")
	g := e.create("a", "b", "c")
	if g.EditInfo != "admins" || g.SendMessages != "all" || g.AddMembers != "admins" || g.CreatedBy != "a" {
		t.Fatalf("defaults: %+v", g)
	}
	info := e.do(e.a.GroupInfo, "GET", "/api/v1/groups/info?group_id="+g.ID, nil, "b")
	if info.Code != 200 || !strings.Contains(info.Body.String(), `"add_members":"admins"`) {
		t.Fatalf("info: %d %s", info.Code, info.Body.String())
	}
	evs := e.events(g.ID, "b")
	if len(evs) != 1 || evs[0].Kind != "create" || evs[0].By != "a" || len(evs[0].Targets) != 2 || evs[0].Names["a"] == "" {
		t.Fatalf("create line: %+v", evs)
	}
	roles := e.roles(g.ID, "c")
	if roles["a"] != "owner" || roles["b"] != "member" {
		t.Fatalf("roles: %+v", roles)
	}
}

func TestG2CreateWithDescriptionAndSettings(t *testing.T) {
	e := newG2Env(t, "a", "b")
	res := e.do(e.a.CreateGroup, "POST", "/api/v1/groups", map[string]any{"name": "Office", "member_ids": []string{"b"}, "description": "Lunch", "send_messages": "admins", "add_members": "all"}, "a")
	if res.Code != 201 {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var g Group
	_ = json.Unmarshal(res.Body.Bytes(), &g)
	if g.Description != "Lunch" || g.SendMessages != "admins" || g.AddMembers != "all" {
		t.Fatalf("create fields: %+v", g)
	}
	bad := e.do(e.a.CreateGroup, "POST", "/api/v1/groups", map[string]any{"name": "X", "member_ids": []string{"b"}, "edit_info": "nobody"}, "a")
	if bad.Code != 400 {
		t.Fatalf("bad setting accepted: %d", bad.Code)
	}
}

func TestG2AddMembersRules(t *testing.T) {
	e := newG2Env(t, "a", "b", "c", "d")
	g := e.create("a", "b")
	add := func(who, target string) *httptest.ResponseRecorder {
		return e.do(e.a.AddGroupMember, "POST", "/api/v1/groups/members?group_id="+g.ID, map[string]any{"device_id": target}, who)
	}
	if res := add("b", "c"); res.Code != 403 || errorCode(res) != groupErrAdminsOnly {
		t.Fatalf("member add should be admins-only: %d %s", res.Code, res.Body.String())
	}
	if res := add("d", "c"); res.Code != 403 || errorCode(res) != groupErrNotMember {
		t.Fatalf("outsider add: %d %s", res.Code, res.Body.String())
	}
	if res := e.do(e.a.UpdateGroup, "PATCH", "/api/v1/groups?group_id="+g.ID, map[string]any{"add_members": "all"}, "a"); res.Code != 200 {
		t.Fatalf("patch: %d %s", res.Code, res.Body.String())
	}
	if res := add("b", "c"); res.Code != 201 {
		t.Fatalf("member add after All members: %d %s", res.Code, res.Body.String())
	}
	evs := e.events(g.ID, "c")
	last := evs[len(evs)-1]
	if last.Kind != "add" || last.By != "b" || last.Targets[0] != "c" {
		t.Fatalf("add line: %+v", last)
	}
	// added_by is kept.
	res := e.do(e.a.GroupMembers, "GET", "/api/v1/groups/members?group_id="+g.ID, nil, "a")
	if !strings.Contains(res.Body.String(), `"added_by":"b"`) {
		t.Fatalf("added_by missing: %s", res.Body.String())
	}
}

func TestG2AddOnlyFriends(t *testing.T) {
	e := newG2Env(t, "a", "b", "x")
	st := e.a.store.DB()
	for id, user := range map[string]string{"a": "ua", "b": "ub", "x": "ux"} {
		if _, err := st.Exec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at) VALUES(?,?,'s','h',1,'active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, user, user); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Exec(`UPDATE devices SET user_id=? WHERE id=?`, user, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Exec(`INSERT INTO account_contacts(user_id,contact_user_id,added_at) VALUES('ua','ub','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// (Account devices need a login session for HTTP calls, so the rule
	// itself is checked here; AddGroupMember calls it for new members.)
	if !e.a.areContacts("a", "b") {
		t.Fatal("friends not seen as contacts")
	}
	if e.a.areContacts("a", "x") {
		t.Fatal("stranger seen as a contact")
	}
	if _, err := st.Exec(`INSERT INTO account_contacts(user_id,contact_user_id,added_at) VALUES('ux','ua','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if !e.a.areContacts("a", "x") {
		t.Fatal("contact in the other direction not seen")
	}
}

func TestG2OnlyAdminsCanSend(t *testing.T) {
	e := newG2Env(t, "a", "b")
	g := e.create("a", "b")
	if res := e.do(e.a.UpdateGroup, "PATCH", "/api/v1/groups?group_id="+g.ID, map[string]any{"send_messages": "admins"}, "b"); res.Code != 403 {
		t.Fatalf("member changed settings: %d", res.Code)
	}
	if res := e.do(e.a.UpdateGroup, "PATCH", "/api/v1/groups?group_id="+g.ID, map[string]any{"send_messages": "admins"}, "a"); res.Code != 200 {
		t.Fatalf("patch: %d %s", res.Code, res.Body.String())
	}
	send := func(who, id string) *httptest.ResponseRecorder {
		return e.do(e.a.CreateGroupMessage, "POST", "/api/v1/group-messages", map[string]any{"id": id, "group_id": g.ID, "body": "hi", "created_at": "2026-11-03T10:00:00Z"}, who)
	}
	if res := send("b", "m-b"); res.Code != 403 || errorCode(res) != groupErrAdminsOnly {
		t.Fatalf("member sent in admins-only group: %d %s", res.Code, res.Body.String())
	}
	if res := send("a", "m-a"); res.Code != 201 {
		t.Fatalf("admin send: %d %s", res.Code, res.Body.String())
	}
	evs := e.events(g.ID, "b")
	last := evs[len(evs)-1]
	if last.Kind != "set" || last.Field != "send_messages" || last.Value != "admins" {
		t.Fatalf("settings line: %+v", last)
	}
	// Nobody can fake a system line.
	res := e.do(e.a.CreateGroupMessage, "POST", "/api/v1/group-messages", map[string]any{"id": "fake", "group_id": g.ID, "body": groupEventPrefix + `{"k":"remove"}`, "created_at": "2026-11-03T10:00:00Z"}, "a")
	if res.Code == 201 {
		t.Fatal("client wrote a system line")
	}
}

func TestG2RolesAndRemove(t *testing.T) {
	e := newG2Env(t, "a", "b", "c", "d")
	g := e.create("a", "b", "c", "d")
	role := func(who, target, r string) *httptest.ResponseRecorder {
		return e.do(e.a.SetGroupRole, "POST", "/api/v1/groups/role?group_id="+g.ID, map[string]any{"device_id": target, "role": r}, who)
	}
	remove := func(who, target string) *httptest.ResponseRecorder {
		return e.do(e.a.RemoveGroupMember, "DELETE", "/api/v1/groups/members?group_id="+g.ID+"&device_id="+target, nil, who)
	}
	if res := role("c", "b", "admin"); res.Code != 403 {
		t.Fatalf("member made admin: %d", res.Code)
	}
	if res := role("a", "b", "admin"); res.Code != 200 {
		t.Fatalf("make admin: %d %s", res.Code, res.Body.String())
	}
	if res := role("b", "c", "admin"); res.Code != 200 {
		t.Fatalf("admin makes admin: %d %s", res.Code, res.Body.String())
	}
	if res := role("b", "c", "member"); res.Code != 403 {
		t.Fatalf("admin dismissed admin: %d", res.Code)
	}
	if res := remove("b", "c"); res.Code != 403 {
		t.Fatalf("admin removed admin: %d", res.Code)
	}
	if res := remove("b", "a"); res.Code != 403 {
		t.Fatalf("owner removed: %d", res.Code)
	}
	if res := remove("b", "d"); res.Code != 200 {
		t.Fatalf("admin removes member: %d %s", res.Code, res.Body.String())
	}
	if res := role("a", "c", "member"); res.Code != 200 {
		t.Fatalf("owner dismisses admin: %d %s", res.Code, res.Body.String())
	}
	roles := e.roles(g.ID, "a")
	if roles["b"] != "admin" || roles["c"] != "member" || roles["d"] != "" {
		t.Fatalf("roles: %+v", roles)
	}
	evs := e.events(g.ID, "a")
	kinds := []string{}
	for _, ev := range evs {
		kinds = append(kinds, ev.Kind)
	}
	if strings.Join(kinds, ",") != "create,admin,admin,remove,unadmin" {
		t.Fatalf("lines: %v", kinds)
	}
	if evs[3].Names["d"] == "" {
		t.Fatalf("removed name missing: %+v", evs[3])
	}
	// The removed phone can no longer read or send.
	if res := e.do(e.a.ListGroupMessages, "GET", "/api/v1/group-messages?group_id="+g.ID, nil, "d"); res.Code != 403 {
		t.Fatalf("removed member reads: %d", res.Code)
	}
}

func TestG2LeaveHandsOverAndLastDeletes(t *testing.T) {
	e := newG2Env(t, "a", "b", "c")
	g := e.create("a", "b", "c")
	if res := e.do(e.a.SetGroupRole, "POST", "/api/v1/groups/role?group_id="+g.ID, map[string]any{"device_id": "c", "role": "admin"}, "a"); res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	res := e.do(e.a.LeaveGroup, "POST", "/api/v1/groups/leave?group_id="+g.ID, nil, "a")
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"owner_id":"c"`) {
		t.Fatalf("owner leave: %d %s", res.Code, res.Body.String())
	}
	roles := e.roles(g.ID, "b")
	if roles["c"] != "owner" || roles["a"] != "" {
		t.Fatalf("hand-over: %+v", roles)
	}
	info := e.do(e.a.GroupInfo, "GET", "/api/v1/groups/info?group_id="+g.ID, nil, "b")
	if !strings.Contains(info.Body.String(), `"owner_id":"c"`) || !strings.Contains(info.Body.String(), `"created_by":"a"`) {
		t.Fatalf("info after leave: %s", info.Body.String())
	}
	evs := e.events(g.ID, "b")
	last := evs[len(evs)-1]
	if last.Kind != "leave" || last.By != "a" || last.Owner != "c" {
		t.Fatalf("leave line: %+v", last)
	}
	// Leaving twice is fine.
	if res := e.do(e.a.LeaveGroup, "POST", "/api/v1/groups/leave?group_id="+g.ID, nil, "a"); res.Code != 200 {
		t.Fatalf("second leave: %d", res.Code)
	}
	// No admin left: the oldest member becomes owner.
	if res := e.do(e.a.LeaveGroup, "POST", "/api/v1/groups/leave?group_id="+g.ID, nil, "c"); res.Code != 200 || !strings.Contains(res.Body.String(), `"owner_id":"b"`) {
		t.Fatalf("leave 2: %d %s", res.Code, res.Body.String())
	}
	res = e.do(e.a.LeaveGroup, "POST", "/api/v1/groups/leave?group_id="+g.ID, nil, "b")
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"deleted":true`) {
		t.Fatalf("last leave: %d %s", res.Code, res.Body.String())
	}
	var n int
	_ = e.a.store.DB().QueryRow(`SELECT COUNT(*) FROM groups WHERE id=?`, g.ID).Scan(&n)
	if n != 0 {
		t.Fatal("empty group kept")
	}
}

func TestG2EditInfoAndPhoto(t *testing.T) {
	e := newG2Env(t, "a", "b", "z")
	g := e.create("a", "b")
	patch := func(who string, body map[string]any) *httptest.ResponseRecorder {
		return e.do(e.a.UpdateGroup, "PATCH", "/api/v1/groups?group_id="+g.ID, body, who)
	}
	if res := patch("b", map[string]any{"name": "Mine"}); res.Code != 403 || errorCode(res) != groupErrAdminsOnly {
		t.Fatalf("member renamed: %d %s", res.Code, res.Body.String())
	}
	if res := patch("a", map[string]any{"name": "Office 2", "description": "Lunch plans"}); res.Code != 200 || !strings.Contains(res.Body.String(), "Office 2") {
		t.Fatalf("rename: %d %s", res.Code, res.Body.String())
	}
	if res := patch("a", map[string]any{"name": ""}); res.Code != 400 {
		t.Fatalf("empty name: %d", res.Code)
	}
	if res := patch("a", map[string]any{"description": strings.Repeat("x", 301)}); res.Code != 400 {
		t.Fatalf("long description: %d", res.Code)
	}
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{1}, 100)...)
	if res := e.raw(e.a.SetGroupPhoto, "PUT", "/api/v1/groups/photo?group_id="+g.ID, jpeg, "b"); res.Code != 403 {
		t.Fatalf("member photo: %d", res.Code)
	}
	if res := e.raw(e.a.SetGroupPhoto, "PUT", "/api/v1/groups/photo?group_id="+g.ID, []byte("png?"), "a"); res.Code != 400 {
		t.Fatalf("not jpeg: %d", res.Code)
	}
	big := append([]byte{0xFF, 0xD8}, bytes.Repeat([]byte{1}, maxGroupPhotoBytes)...)
	if res := e.raw(e.a.SetGroupPhoto, "PUT", "/api/v1/groups/photo?group_id="+g.ID, big, "a"); res.Code != 413 {
		t.Fatalf("too big: %d", res.Code)
	}
	if res := e.raw(e.a.SetGroupPhoto, "PUT", "/api/v1/groups/photo?group_id="+g.ID, jpeg, "a"); res.Code != 200 || !strings.Contains(res.Body.String(), `"photo_version":1`) {
		t.Fatalf("photo: %d %s", res.Code, res.Body.String())
	}
	got := e.do(e.a.GroupPhoto, "GET", "/api/v1/groups/photo?group_id="+g.ID, nil, "b")
	if got.Code != 200 || !bytes.Equal(got.Body.Bytes(), jpeg) || got.Header().Get("ETag") != "1" {
		t.Fatalf("photo get: %d", got.Code)
	}
	if res := e.do(e.a.GroupPhoto, "GET", "/api/v1/groups/photo?group_id="+g.ID, nil, "z"); res.Code != 403 {
		t.Fatalf("outsider photo: %d", res.Code)
	}
	// "All members" may edit info.
	if res := patch("a", map[string]any{"edit_info": "all"}); res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	if res := e.raw(e.a.SetGroupPhoto, "DELETE", "/api/v1/groups/photo?group_id="+g.ID, nil, "b"); res.Code != 200 || !strings.Contains(res.Body.String(), `"photo_version":2`) {
		t.Fatalf("member removes photo: %d %s", res.Code, res.Body.String())
	}
	kinds := []string{}
	for _, ev := range e.events(g.ID, "a") {
		kinds = append(kinds, ev.Kind)
	}
	if strings.Join(kinds, ",") != "create,name,desc,photo,set,photo_removed" {
		t.Fatalf("lines: %v", kinds)
	}
}

func TestG2SyncCarriesInfo(t *testing.T) {
	e := newG2Env(t, "a", "b")
	g := e.create("a", "b")
	if res := e.do(e.a.UpdateGroup, "PATCH", "/api/v1/groups?group_id="+g.ID, map[string]any{"description": "Hello"}, "a"); res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	res := e.do(e.a.Sync, "GET", "/api/v1/sync", nil, "b")
	if res.Code != 200 {
		t.Fatalf("sync: %d %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, `"description":"Hello"`) || !strings.Contains(body, `"added_by":"a"`) || !strings.Contains(body, "llev:v1:") {
		t.Fatalf("sync lacks G2 fields: %s", body)
	}
}
