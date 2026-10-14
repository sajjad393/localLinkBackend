package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Conversation review is the most sensitive admin capability in this server:
// an owner can read what users said to each other. These tests pin down the
// three properties that make that defensible — only the owner gets in, the
// listing exposes no message text, and every thread read is audited.

// adminAccountWithRole inserts an administrator with the given role and a live
// session, returning the bearer token. Non-owner roles exist to prove they are
// refused the conversation endpoints.
func adminAccountWithRole(t *testing.T, api *API, role string) string {
	t.Helper()
	db := api.store.DB()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	adminID := "admin-" + role
	if _, err := db.Exec(`INSERT INTO admin_accounts(id,username,password_salt,password_hash,password_iterations,role,status,created_at,updated_at) VALUES(?,?,?,?,?,?,'active',?,?)`,
		adminID, role+"-admin", "salt", "hash", 1, role, now, now); err != nil {
		t.Fatalf("insert %s admin: %v", role, err)
	}
	token := "token-" + role
	if _, err := db.Exec(`INSERT INTO admin_sessions(id,admin_id,token_hash,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?)`,
		"session-"+role, adminID, tokenHash(token), now, now, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert %s session: %v", role, err)
	}
	return token
}

// seedConversation creates two devices on two accounts and a short exchange
// between them.
func seedConversation(t *testing.T, api *API) (string, string) {
	t.Helper()
	db := api.store.DB()
	now := time.Now().UTC()
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	for i, name := range []string{"amina", "bilal"} {
		id := []string{"user-a", "user-b"}[i]
		mustExec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at) VALUES(?,?,'s','h',1,'active',?,?)`,
			id, name, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	}
	deviceA, deviceB := "device-a", "device-b"
	mustExec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,status,user_id) VALUES(?,?,?,?,?,'active','user-a')`,
		deviceA, "Amina phone", "android", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	mustExec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,status,user_id) VALUES(?,?,?,?,?,'active','user-b')`,
		deviceB, "Bilal laptop", "windows", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	pairs := []struct {
		id, from, to, body string
		offset             time.Duration
	}{
		{"m1", deviceA, deviceB, "are we still meeting at six", -30 * time.Minute},
		{"m2", deviceB, deviceA, "yes, same place", -29 * time.Minute},
		{"m3", deviceA, deviceB, "bring the spare keys", -28 * time.Minute},
	}
	// server_seq carries a unique index, so each seeded message needs its own.
	for i, p := range pairs {
		mustExec(`INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status,server_seq) VALUES(?,?,?,?,?,'delivered',?)`,
			p.id, p.from, p.to, p.body, now.Add(p.offset).Format(time.RFC3339Nano), i+1)
	}
	return deviceA, deviceB
}

func adminGet(t *testing.T, handler http.HandlerFunc, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("X-Admin-Token", "test-admin-token")
	}
	res := httptest.NewRecorder()
	handler(res, req)
	return res
}

func TestAdminConversationsAreOwnerOnly(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	seedConversation(t, api)

	path := "/api/v1/admin/conversation?user_a=device-a&user_b=device-b"
	for _, role := range []string{"operator", "security", "auditor"} {
		token := adminAccountWithRole(t, api, role)
		for _, call := range []struct {
			name    string
			handler http.HandlerFunc
			path    string
		}{
			{"list", api.AdminConversations, "/api/v1/admin/conversations"},
			{"direct thread", api.AdminConversation, path},
			{"group thread", api.AdminGroupConversation, "/api/v1/admin/group-conversation?group_id=g1"},
		} {
			res := adminGet(t, call.handler, call.path, token)
			if res.Code != http.StatusForbidden {
				t.Errorf("%s as %s: status = %d, want 403 (only the owner may read conversations)", call.name, role, res.Code)
			}
			if body := res.Body.String(); strings.Contains(body, "meeting at six") || strings.Contains(body, "spare keys") {
				t.Errorf("%s as %s leaked message text in the refusal", call.name, role)
			}
		}
	}
}

func TestAdminConversationReturnsTextAndAudits(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	deviceA, deviceB := seedConversation(t, api)

	res := adminGet(t, api.AdminConversation, "/api/v1/admin/conversation?user_a="+deviceA+"&user_b="+deviceB, "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
	}
	var body struct {
		Kind         string `json:"kind"`
		Participants []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"participants"`
		Messages []struct {
			Body, SenderName, CreatedAt string
		} `json:"messages"`
		ContentIncluded bool   `json:"content_included"`
		Audited         bool   `json:"audited"`
		Access          string `json:"access"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.ContentIncluded || !body.Audited || body.Access != "owner" {
		t.Errorf("response should declare content, audit and access: %+v", body)
	}
	if len(body.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(body.Messages))
	}
	// Newest first, so the console can present it in order after reversing.
	if body.Messages[0].Body != "bring the spare keys" {
		t.Errorf("first message = %q, want the newest", body.Messages[0].Body)
	}
	if len(body.Participants) != 2 || body.Participants[0].Username != "amina" {
		t.Errorf("participants should name both accounts: %+v", body.Participants)
	}

	// Reversed arguments must reach the same conversation.
	res2 := adminGet(t, api.AdminConversation, "/api/v1/admin/conversation?user_a="+deviceB+"&user_b="+deviceA, "")
	if res2.Code != http.StatusOK || res2.Body.String() != res.Body.String() {
		t.Errorf("reversing user_a and user_b changed the result")
	}

	// The read is recorded.
	var count int
	if err := api.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action='admin_conversation_viewed'`).Scan(&count); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if count != 2 {
		t.Errorf("audit rows = %d, want 2 (one per read)", count)
	}
	var detail string
	if err := api.store.DB().QueryRow(`SELECT detail FROM admin_audit WHERE action='admin_conversation_viewed' LIMIT 1`).Scan(&detail); err != nil {
		t.Fatalf("read audit detail: %v", err)
	}
	if detail != "messages=3" {
		t.Errorf("audit detail = %q, want the number of messages returned", detail)
	}
}

func TestAdminConversationListingCarriesNoText(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	seedConversation(t, api)

	res := adminGet(t, api.AdminConversations, "/api/v1/admin/conversations", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
	}
	raw := res.Body.String()
	for _, secret := range []string{"meeting at six", "same place", "spare keys"} {
		if strings.Contains(raw, secret) {
			t.Errorf("the listing leaked message text: %q", secret)
		}
	}
	var body struct {
		Conversations []struct {
			Kind         string `json:"kind"`
			MessageCount int    `json:"message_count"`
			Participants []struct {
				Username string `json:"username"`
				Name     string `json:"name"`
			} `json:"participants"`
		} `json:"conversations"`
		ContentIncluded bool `json:"content_included"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ContentIncluded {
		t.Error("the listing must declare that it carries no content")
	}
	if len(body.Conversations) != 1 {
		t.Fatalf("conversations = %d, want 1", len(body.Conversations))
	}
	c := body.Conversations[0]
	if c.Kind != "direct" || c.MessageCount != 3 {
		t.Errorf("summary = %+v, want a direct conversation of 3 messages", c)
	}
	// A conversation is one thread regardless of who wrote which message, and
	// people are named rather than shown as device ids.
	if len(c.Participants) != 2 || c.Participants[0].Username != "amina" || c.Participants[1].Username != "bilal" {
		t.Errorf("participants = %+v, want amina and bilal", c.Participants)
	}

	// Listing is metadata only, so it is not audited.
	var count int
	if err := api.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action='admin_conversation_viewed'`).Scan(&count); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if count != 0 {
		t.Errorf("audit rows = %d, want 0 for a metadata-only listing", count)
	}
}

func TestAdminConversationPagination(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	deviceA, deviceB := seedConversation(t, api)
	now := time.Now().UTC()
	for i := 0; i < 12; i++ {
		if _, err := api.store.DB().Exec(`INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status) VALUES(?,?,?,?,?,'sent')`,
			"bulk-"+string(rune('a'+i)), deviceA, deviceB, "bulk message", now.Add(time.Duration(i)*time.Minute).Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("seed bulk: %v", err)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		path := "/api/v1/admin/conversation?user_a=" + deviceA + "&user_b=" + deviceB + "&limit=5"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		res := adminGet(t, api.AdminConversation, path, "")
		if res.Code != http.StatusOK {
			t.Fatalf("page %d: status = %d: %s", pages, res.Code, res.Body.String())
		}
		var body struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, m := range body.Messages {
			if seen[m.ID] {
				t.Fatalf("message %s appeared on two pages", m.ID)
			}
			seen[m.ID] = true
		}
		pages++
		if !body.HasMore {
			break
		}
		if body.NextCursor == "" {
			t.Fatal("has_more is set but no cursor was returned")
		}
		cursor = body.NextCursor
		if pages > 20 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 15 {
		t.Errorf("walked %d messages, want all 15", len(seen))
	}
}

func TestAdminGroupConversationIsReadableAndAudited(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	deviceA, _ := seedConversation(t, api)
	db := api.store.DB()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO groups(id,name,owner_id,created_at) VALUES('g1','Site team',?,?)`, deviceA, now); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO group_members(group_id,device_id,role,joined_at) VALUES('g1',?,'owner',?)`, deviceA, now); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO group_messages(id,group_id,sender_id,body,created_at,server_seq) VALUES('gm1','g1',?,'generator is back on',?,1)`, deviceA, now); err != nil {
		t.Fatalf("seed group message: %v", err)
	}

	res := adminGet(t, api.AdminGroupConversation, "/api/v1/admin/group-conversation?group_id=g1", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "generator is back on") {
		t.Error("group message text missing")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action='admin_group_conversation_viewed'`).Scan(&count); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if count != 1 {
		t.Errorf("group audit rows = %d, want 1", count)
	}

	// The listing shows the group beside direct conversations.
	list := adminGet(t, api.AdminConversations, "/api/v1/admin/conversations", "")
	if !strings.Contains(list.Body.String(), "Site team") {
		t.Error("group conversation missing from the listing")
	}
	if strings.Contains(list.Body.String(), "generator is back on") {
		t.Error("the listing leaked group message text")
	}

	// An unknown group is a clear 404, not an empty success.
	missing := adminGet(t, api.AdminGroupConversation, "/api/v1/admin/group-conversation?group_id=nope", "")
	if missing.Code != http.StatusNotFound {
		t.Errorf("unknown group: status = %d, want 404", missing.Code)
	}
}

func TestAdminConversationRejectsBadRequests(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	seedConversation(t, api)

	cases := []struct {
		path string
		want int
		why  string
	}{
		{"/api/v1/admin/conversation", 400, "no participants"},
		{"/api/v1/admin/conversation?user_a=device-a", 400, "one participant"},
		{"/api/v1/admin/conversation?user_a=device-a&user_b=device-a", 400, "same device twice"},
		{"/api/v1/admin/conversation?user_a=device-a&user_b=device-b&limit=0", 400, "limit below range"},
		{"/api/v1/admin/conversation?user_a=device-a&user_b=device-b&limit=5000", 400, "limit above range"},
		{"/api/v1/admin/conversation?user_a=device-a&user_b=device-b&cursor=not-a-cursor", 400, "malformed cursor"},
		{"/api/v1/admin/group-conversation", 400, "no group"},
	}
	for _, c := range cases {
		handler := http.HandlerFunc(api.AdminConversation)
		if strings.HasPrefix(c.path, "/api/v1/admin/group-conversation") {
			handler = api.AdminGroupConversation
		}
		if res := adminGet(t, handler, c.path, ""); res.Code != c.want {
			t.Errorf("%s: status = %d, want %d (%s)", c.why, res.Code, c.want, res.Body.String())
		}
	}

	// A refused or malformed request must not leave an audit entry claiming a
	// conversation was read.
	var count int
	if err := api.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action LIKE 'admin_%conversation_viewed'`).Scan(&count); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if count != 0 {
		t.Errorf("audit rows = %d, want 0 for rejected requests", count)
	}
}

func TestAdminPrivacyPolicyStatesMessageAccess(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()

	res := adminGet(t, api.AdminPrivacyPolicy, "/api/v1/admin/privacy", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}
	var policy struct {
		PrivateMessageBodiesExposed  bool   `json:"private_message_bodies_exposed"`
		MessageBodyAccessRole        string `json:"message_body_access_role"`
		MessageBodyAccessAudited     bool   `json:"message_body_access_audited"`
		AdministrativeActionsAudited bool   `json:"administrative_actions_audited"`
		PrivateKeysExposed           bool   `json:"private_keys_exposed"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &policy); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !policy.PrivateMessageBodiesExposed {
		t.Error("the policy must admit that an owner can read message bodies")
	}
	if policy.MessageBodyAccessRole != "owner" {
		t.Errorf("access role = %q, want owner", policy.MessageBodyAccessRole)
	}
	if !policy.MessageBodyAccessAudited {
		t.Error("the policy must state that reads are audited")
	}
	if policy.PrivateKeysExposed || !policy.AdministrativeActionsAudited {
		t.Error("the rest of the policy should be unchanged")
	}
}
