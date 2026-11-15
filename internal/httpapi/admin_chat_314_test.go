package httpapi

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Bundle 314 (TODO_314): own chats refused, one open request per chat,
// nearby group notes.

func makeAdminUser314(t *testing.T, s *blocks212, userID string) {
	t.Helper()
	if _, err := s.api.store.DB().Exec(`UPDATE admin_accounts SET user_id=?`, userID); err != nil {
		t.Fatal(err)
	}
	// A phone admin session (Bundle 214): it is tied to the admin's phone.
	if _, err := s.api.store.DB().Exec(`UPDATE admin_sessions SET device_id=?`, s.adminDev.Device.ID); err != nil {
		t.Fatal(err)
	}
}

func refused314(t *testing.T, s *blocks212, in map[string]any) (int, map[string]any) {
	t.Helper()
	res := call221(t, s.api.AdminChatRequestCreate, http.MethodPost, "/api/v1/admin/chat-requests", "", in, owner221(s))
	out := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out
}

func serverGroup314(t *testing.T, s *blocks212, id, name string, devices ...string) {
	t.Helper()
	now := nowStamp()
	if _, err := s.api.store.DB().Exec(`INSERT INTO groups(id,name,owner_id,created_at) VALUES(?,?,?,?)`, id, name, devices[0], now); err != nil {
		t.Fatal(err)
	}
	for _, d := range devices {
		if _, err := s.api.store.DB().Exec(`INSERT INTO group_members(group_id,device_id,role,joined_at) VALUES(?,?,'member',?)`, id, d, now); err != nil {
			t.Fatal(err)
		}
	}
}

func (p phone220) groupNote314(gid, name string, members []string, at time.Time) signedBlob {
	raw, _ := json.Marshal(groupNote314{T: groupNoteType314, V: 1, GID: gid, Name: name, Members: members,
		MadeAt: "2026-11-15T10:00:00Z", From: p.auth.Device.ID, At: at.UTC().Format(time.RFC3339Nano)})
	return signedBlob{Body: string(raw), Sig: b64url(ed25519.Sign(p.sign, raw))}
}

func saveSigningKey314(t *testing.T, s *blocks212, p phone220) {
	t.Helper()
	if _, err := s.api.store.DB().Exec(`INSERT OR REPLACE INTO device_signing_keys(device_id,signing_key,first_seen_at) VALUES(?,?,?)`,
		p.auth.Device.ID, p.signingKey(), nowStamp()); err != nil {
		t.Fatal(err)
	}
}

func postNotes314(t *testing.T, s *blocks212, from phone220, notes ...signedBlob) map[string]any {
	t.Helper()
	res := doJSON(t, s.api.AdminGroupNotes, http.MethodPost, "/api/v1/admin/group-notes", map[string]any{"notes": notes}, directoryHeaders(from.auth))
	if res.Code != http.StatusOK {
		t.Fatalf("notes %d %s", res.Code, res.Body.String())
	}
	out := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return out
}

func adminGroups314(t *testing.T, s *blocks212) (groups []AdminGroup, nearby []AdminNearbyGroup) {
	t.Helper()
	res := call221(t, s.api.AdminGroups, http.MethodGet, "/api/v1/admin/groups?limit=200", "", nil, owner221(s))
	if res.Code != http.StatusOK {
		t.Fatalf("groups %d %s", res.Code, res.Body.String())
	}
	var out struct {
		Groups []AdminGroup       `json:"groups"`
		Nearby []AdminNearbyGroup `json:"nearby"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return out.Groups, out.Nearby
}

func TestOwnChatIsRefusedOnlyForTheOwnersOwnUser314(t *testing.T) {
	s := setup212(t)
	addPhoneKeys220(t, s.api, s.adminDev)
	addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	serverGroup314(t, s, "g-mine", "Family", s.adminDev.Device.ID, s.ali.Device.ID)
	serverGroup314(t, s, "g-other", "Friends", s.ali.Device.ID, s.bilal.Device.ID)

	// A password-only web owner has no app user: nothing is "mine".
	if code, out := refused314(t, s, map[string]any{"kind": "direct", "user_a": s.adminDev.Account.ID, "user_b": s.ali.Account.ID}); code != http.StatusCreated {
		t.Fatalf("password-only owner: %d %v", code, out)
	}

	makeAdminUser314(t, s, s.adminDev.Account.ID)
	for _, in := range []map[string]any{
		{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.adminDev.Account.ID, "range": "7"},
		{"kind": "direct", "user_a": s.adminDev.Device.ID, "user_b": s.bilal.Device.ID}, // device ids too
		{"kind": "group", "group_id": "g-mine"},
	} {
		code, out := refused314(t, s, in)
		if code != http.StatusConflict || out["code"] != ownChatCode314 || out["error_code"] != ownChatCode314 || out["error"] != ownChatText314 {
			t.Fatalf("%v -> %d %v", in, code, out)
		}
	}
	if out := ownChatText314; out != "You are in this chat. It's already on your phone." {
		t.Fatalf("words %q", out)
	}
	// Other chats still work.
	if code, out := refused314(t, s, map[string]any{"kind": "group", "group_id": "g-other"}); code != http.StatusCreated {
		t.Fatalf("other group: %d %v", code, out)
	}
	// Regression: bad input is still a 400 before anything else.
	if code, _ := refused314(t, s, map[string]any{"kind": "direct", "user_a": s.adminDev.Account.ID, "user_b": s.adminDev.Account.ID}); code != http.StatusBadRequest {
		t.Fatalf("same person: %d", code)
	}
	// The group list says which groups are mine; member phones for the Owner.
	groups, _ := adminGroups314(t, s)
	seen := 0
	for _, g := range groups {
		switch g.ID {
		case "g-mine":
			seen++
			if !g.Me || len(g.Devices) != 2 {
				t.Fatalf("mine %+v", g)
			}
		case "g-other":
			seen++
			if g.Me || len(g.Devices) != 2 {
				t.Fatalf("other %+v", g)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("groups %+v", groups)
	}
}

func TestOneOpenRequestPerChatAnyRange314(t *testing.T) {
	s := setup212(t)
	addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	first, _ := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})

	// Same 2 people, other order, other range, device ids: still blocked.
	for _, in := range []map[string]any{
		{"kind": "direct", "user_a": s.bilal.Account.ID, "user_b": s.ali.Account.ID, "range": "7"},
		{"kind": "direct", "user_a": s.bilal.Device.ID, "user_b": s.ali.Device.ID, "range": "dates", "from": "2026-11-01", "to": "2026-11-09"},
	} {
		code, out := refused314(t, s, in)
		if code != http.StatusConflict || out["code"] != alreadyAskedCode314 || out["open_id"] != first || out["error"] != alreadyAskedText314 {
			t.Fatalf("%v -> %d %v", in, code, out)
		}
	}
	// A Ready answer (all parts here) does not block.
	db := s.api.store.DB()
	if _, err := db.Exec(`INSERT INTO admin_action_answer_meta(answer_id,action_id,device_id,of,made_at) VALUES('ans-1',?,?,1,?)`, first, s.ali.Device.ID, nowStamp()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO admin_action_answers(answer_id,action_id,part,of,sha256,body,sig,at) VALUES('ans-1',?,1,1,'x','{}','s',?)`, first, nowStamp()); err != nil {
		t.Fatal(err)
	}
	second, _ := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID, "range": "30"})
	// The new one blocks again until it is cancelled.
	if code, out := refused314(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID}); code != http.StatusConflict || out["open_id"] != second {
		t.Fatalf("second open: %d %v", code, out)
	}
	if res := call221(t, s.api.AdminActionCancel, http.MethodPost, "/x", second, nil, owner221(s)); res.Code != http.StatusOK {
		t.Fatalf("cancel %d %s", res.Code, res.Body.String())
	}
	newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	// Other chats are not blocked.
	serverGroup314(t, s, "g1", "Site", s.ali.Device.ID, s.bilal.Device.ID)
	g1, _ := newChatRequest221(t, s, map[string]any{"kind": "group", "group_id": "g1"})
	if code, out := refused314(t, s, map[string]any{"kind": "group", "group_id": "g1", "range": "7"}); code != http.StatusConflict || out["open_id"] != g1 {
		t.Fatalf("group open: %d %v", code, out)
	}
	// A done request does not block.
	if _, err := db.Exec(`UPDATE admin_actions SET state='done' WHERE id=?`, g1); err != nil {
		t.Fatal(err)
	}
	newChatRequest221(t, s, map[string]any{"kind": "group", "group_id": "g1"})
}

func TestNearbyGroupNotesSignedMembersOnlyAndAskable314(t *testing.T) {
	s := setup212(t)
	admin := addPhoneKeys220(t, s.api, s.adminDev)
	ali := addPhoneKeys220(t, s.api, s.ali)
	bilal := addPhoneKeys220(t, s.api, s.bilal)
	saveSigningKey314(t, s, ali)
	members := []string{s.ali.Device.ID, s.bilal.Device.ID}
	now := time.Now()

	// Bilal (key not known yet) carries Ali's note: Ali's key is known -> kept.
	out := postNotes314(t, s, bilal, ali.groupNote314("pg-1", "Study group", members, now))
	if out["kept"].(float64) != 1 {
		t.Fatalf("kept %v", out)
	}
	// Refused: not a member, a bad sign, a carried note without a known key.
	stranger := ali.groupNote314("pg-2", "Fake", []string{s.bilal.Device.ID}, now)
	bad := ali.groupNote314("pg-3", "Bad", members, now)
	bad.Sig = b64url(make([]byte, 64))
	carried := bilal.groupNote314("pg-4", "Carried", members, now)
	out = postNotes314(t, s, admin, stranger, bad, carried)
	ref, _ := out["refused"].(map[string]any)
	if out["kept"].(float64) != 0 || ref["not_member"] != 1.0 || ref["bad_sig"] != 1.0 || ref["unknown_key"] != 1.0 {
		t.Fatalf("refused %v", out)
	}
	// The phone itself may send its own note before its key is saved.
	if out = postNotes314(t, s, bilal, bilal.groupNote314("pg-4", "Carried", members, now)); out["kept"].(float64) != 1 {
		t.Fatalf("own note %v", out)
	}
	// An older note never replaces a newer one.
	postNotes314(t, s, ali, ali.groupNote314("pg-1", "Old name", members, now.Add(-time.Hour)))
	if name, _, _ := s.api.reportedGroup314("pg-1"); name != "Study group" {
		t.Fatalf("name %q", name)
	}
	// No messages or keys in the table: only the note fields.
	var body string
	_ = s.api.store.DB().QueryRow(`SELECT body FROM group_notes_314 WHERE gid='pg-1'`).Scan(&body)
	if strings.Contains(body, "body\":") || strings.Contains(body, "key") {
		t.Fatalf("note body %s", body)
	}

	// The Owner's list shows them as nearby groups (count + phones).
	_, nearby := adminGroups314(t, s)
	if len(nearby) != 2 || nearby[0].Members != 2 || len(nearby[0].Devices) != 2 || nearby[0].Me {
		t.Fatalf("nearby %+v", nearby)
	}
	// Get chat for a reported group: the card goes to the note's phones.
	_, card := newChatRequest221(t, s, map[string]any{"kind": "group", "group_id": "pg-1"})
	if len(card.To) != 2 {
		t.Fatalf("boxes %d", len(card.To))
	}
	// A group the server knows itself is not listed twice.
	serverGroup314(t, s, "pg-4", "Carried", s.ali.Device.ID, s.bilal.Device.ID)
	if _, nearby = adminGroups314(t, s); len(nearby) != 1 || nearby[0].ID != "pg-1" {
		t.Fatalf("nearby after server group %+v", nearby)
	}
	// The Owner is a member of a reported group: `me`, and Get chat refused.
	makeAdminUser314(t, s, s.adminDev.Account.ID)
	saveSigningKey314(t, s, admin)
	postNotes314(t, s, admin, admin.groupNote314("pg-5", "Mine", []string{s.adminDev.Device.ID, s.ali.Device.ID}, now))
	_, nearby = adminGroups314(t, s)
	mine := false
	for _, g := range nearby {
		mine = mine || (g.ID == "pg-5" && g.Me)
	}
	if !mine {
		t.Fatalf("pg-5 not marked me: %+v", nearby)
	}
	if code, o := refused314(t, s, map[string]any{"kind": "group", "group_id": "pg-5"}); code != http.StatusConflict || o["code"] != ownChatCode314 {
		t.Fatalf("own nearby group: %d %v", code, o)
	}
	// Device sign-in is required.
	if res := doJSON(t, s.api.AdminGroupNotes, http.MethodPost, "/api/v1/admin/group-notes", map[string]any{"notes": []any{}}); res.Code != http.StatusUnauthorized {
		t.Fatalf("no auth %d", res.Code)
	}
}

func TestConversationsMarkMyChats314(t *testing.T) {
	s := setup212(t)
	db := s.api.store.DB()
	now := nowStamp()
	a, b := s.adminDev.Device.ID, s.ali.Device.ID
	if a > b {
		a, b = b, a
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO conv_index(kind,a,b,group_id,first_at,last_at,count) VALUES('direct',?,?,'',?,?,1)`, []any{a, b, now, now}},
		{`INSERT INTO conv_index(kind,a,b,group_id,first_at,last_at,count) VALUES('direct',?,?,'',?,?,1)`, []any{s.ali.Device.ID, s.bilal.Device.ID, now, now}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	serverGroup314(t, s, "g-c", "Team", s.adminDev.Device.ID, s.bilal.Device.ID)
	if _, err := db.Exec(`INSERT INTO conv_index(kind,a,b,group_id,first_at,last_at,count) VALUES('group','','','g-c',?,?,1)`, now, now); err != nil {
		t.Fatal(err)
	}
	list := func() []conversationSummary {
		res := call221(t, s.api.AdminConversations, http.MethodGet, "/api/v1/admin/conversations", "", nil, owner221(s))
		if res.Code != http.StatusOK {
			t.Fatalf("list %d %s", res.Code, res.Body.String())
		}
		var out struct {
			Conversations []conversationSummary `json:"conversations"`
		}
		_ = json.Unmarshal(res.Body.Bytes(), &out)
		return out.Conversations
	}
	for _, c := range list() {
		if c.Me {
			t.Fatalf("password-only owner: nothing is mine, got %+v", c)
		}
	}
	makeAdminUser314(t, s, s.adminDev.Account.ID)
	mine := 0
	for _, c := range list() {
		if c.Me {
			mine++
		}
		if c.Kind == "direct" && c.Me != (c.Participants[0].ID == s.adminDev.Device.ID || c.Participants[1].ID == s.adminDev.Device.ID) {
			t.Fatalf("direct %+v", c)
		}
	}
	if mine != 2 {
		t.Fatalf("mine=%d", mine)
	}
}

func TestWebHidesOwnChatsAndOpensTheOpenRequest314(t *testing.T) {
	views := readAdminWeb314(t, "views.js")
	app := readAdminWeb314(t, "app.js")
	for _, want := range []string{
		"filter((u) => !meId || u.id !== meId)",
		"filter((g) => !g.me)",
		"Nearby \\u00b7 ${g.members} members",
		"if (!q && r && r.me) button = ''",
		"} else if (r && r.me) {",
		"d314.code === 'already_asked' && d314.open_id",
		"openViewer(d314.open_id)",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
	if !strings.Contains(app, "failure.data = data;") {
		t.Fatal("app.js must keep the error body")
	}
}

func readAdminWeb314(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("adminweb/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
