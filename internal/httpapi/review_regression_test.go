package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"locallink/backend/internal/store"
)

func TestWebSocketUpgradeHeaderValidationHelpers(t *testing.T) {
	if !headerHasToken([]string{"keep-alive, Upgrade"}, "upgrade") {
		t.Fatal("expected Upgrade token")
	}
	if headerHasToken([]string{"keep-alive"}, "upgrade") {
		t.Fatal("unexpected Upgrade token")
	}
	key := make([]byte, 16)
	encoded := base64.StdEncoding.EncodeToString(key)
	if !validWebSocketKey(encoded) {
		t.Fatal("valid websocket key rejected")
	}
	if validWebSocketKey(base64.StdEncoding.EncodeToString(make([]byte, 15))) {
		t.Fatal("invalid websocket key accepted")
	}
}

func TestMessageAttachmentRetryIsIdempotent(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	api := New(st)
	now := "2026-09-20T00:00:00Z"
	for _, id := range []string{"a", "b"} {
		if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash) VALUES(?,?,?,?,?,?)`, id, id, "android", now, now, tokenHash(id+"-token")); err != nil {
			t.Fatal(err)
		}
	}
	fileID := "file-1"
	if _, err := st.DB().Exec(`INSERT INTO files(id,owner_id,original_name,content_type,size,sha256,storage_path,created_at,status) VALUES(?,?,?,?,?,?,?,?,?)`, fileID, "a", "x.txt", "text/plain", 1, "sha", "/tmp/x", now, "ready"); err != nil {
		t.Fatal(err)
	}
	m, existing, err := api.insertMessage("a", "b", "hello", "m1", []string{fileID})
	if err != nil || existing {
		t.Fatalf("first insert failed: %+v existing=%v err=%v", m, existing, err)
	}
	m2, existing, err := api.insertMessage("a", "b", "hello", "m1", []string{fileID})
	if err != nil || !existing || m2.ID != "m1" {
		t.Fatalf("retry should be idempotent: %+v existing=%v err=%v", m2, existing, err)
	}
	var count int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM message_attachments WHERE message_id=?`, "m1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one attachment, got %d", count)
	}
}

func TestGroupRemoveReAddCollapsesOfflineMembershipEvents(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	api := New(st)
	register := func(id string) string {
		res := doJSON(t, api.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": id, "name": id, "platform": "android"}, nil)
		if res.Code != http.StatusCreated {
			t.Fatalf("register %s: %d %s", id, res.Code, res.Body.String())
		}
		var p struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p.Token
	}
	aToken := register("a")
	bToken := register("b")
	headersA := map[string]string{"Authorization": "Bearer " + aToken, "X-Device-ID": "a", "Content-Type": "application/json"}
	headersB := map[string]string{"Authorization": "Bearer " + bToken, "X-Device-ID": "b"}
	create := doJSON(t, api.CreateGroup, http.MethodPost, "/api/v1/groups", map[string]any{"name": "Test", "member_ids": []string{"b"}}, headersA)
	if create.Code != http.StatusCreated {
		t.Fatalf("create group: %d %s", create.Code, create.Body.String())
	}
	var g Group
	if err := json.Unmarshal(create.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}

	initial := doJSON(t, api.Sync, http.MethodGet, "/api/v1/sync", nil, headersB)
	if initial.Code != http.StatusOK {
		t.Fatalf("initial sync: %d %s", initial.Code, initial.Body.String())
	}
	var first SyncResponse
	if err := json.Unmarshal(initial.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Groups) != 1 || first.Groups[0].ID != g.ID {
		t.Fatalf("expected initial group upsert, got %+v", first.Groups)
	}
	cursor := first.NextCursor
	if cursor == "" {
		t.Fatal("initial sync did not return cursor")
	}

	removed := doJSON(t, api.RemoveGroupMember, http.MethodDelete, "/api/v1/groups/members?group_id="+g.ID+"&device_id=b", nil, headersA)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove member: %d %s", removed.Code, removed.Body.String())
	}
	added := doJSON(t, api.AddGroupMember, http.MethodPost, "/api/v1/groups/members?group_id="+g.ID, map[string]any{"device_id": "b"}, headersA)
	if added.Code != http.StatusCreated {
		t.Fatalf("re-add member: %d %s", added.Code, added.Body.String())
	}

	query := "/api/v1/sync?cursor=" + cursor
	res := doJSON(t, api.Sync, http.MethodGet, query, nil, headersB)
	if res.Code != http.StatusOK {
		t.Fatalf("post re-add sync: %d %s", res.Code, res.Body.String())
	}
	var second SyncResponse
	if err := json.Unmarshal(res.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.RemovedGroupIDs) != 0 {
		t.Fatalf("stale removal event survived re-add: %+v", second.RemovedGroupIDs)
	}
	if len(second.Groups) != 1 || second.Groups[0].ID != g.ID {
		t.Fatalf("expected latest group upsert after re-add, got %+v", second.Groups)
	}
}

func TestSyncFiltersRemovedGroupMessages(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	api := New(st)
	now := "2026-09-20T00:00:00Z"
	for _, id := range []string{"a", "b"} {
		if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash) VALUES(?,?,?,?,?,?)`, id, id, "android", now, now, tokenHash(id+"-token")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DB().Exec(`INSERT INTO groups(id,name,owner_id,created_at,key_version,rekey_required) VALUES('g','Test','a',?,?,?)`, now, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO group_members(group_id,device_id,role,joined_at) VALUES('g','a','owner',?),('g','b','member',?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.insertGroupMessage("a", "g", `gme:v1:{"version":1,"type":"group_message","key_version":1}`, "2026-09-20T00:00:00Z", "gm1", nil); err != nil {
		t.Fatal(err)
	}
	// Simulate the removal transaction directly; this is the same invariant the endpoint enforces.
	tx, err := st.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM group_members WHERE group_id='g' AND device_id='b'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM sync_events WHERE device_id='b' AND event_type='group_message' AND event_id IN (SELECT id FROM group_messages WHERE group_id='g')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO sync_events(device_id,event_type,event_id) VALUES('b','group_removed','g')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	resp := doJSON(t, api.Sync, http.MethodGet, "/api/v1/sync", nil, map[string]string{"Authorization": "Bearer " + "b-token", "X-Device-ID": "b"})
	if resp.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", resp.Code, resp.Body.String())
	}
	var payload SyncResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.GroupMessages) != 0 {
		t.Fatalf("removed member received group messages: %+v", payload.GroupMessages)
	}
	if len(payload.RemovedGroupIDs) != 1 || payload.RemovedGroupIDs[0] != "g" {
		t.Fatalf("expected group removal event: %+v", payload.RemovedGroupIDs)
	}
}

func TestSniffContentType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(path, []byte("hello world"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := sniffContentType(path)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("expected detected content type")
	}
}
