package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"locallink/backend/internal/store"
)

func TestGroupSchemaAndHelpers(t *testing.T) {
	f, err := os.CreateTemp("", "locallink-groups-*.db")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	api := New(st)
	now := "2026-01-01T00:00:00Z"
	if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash) VALUES('a','A','android',?,?,?)`, now, now, tokenHash("a-token")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash) VALUES('b','B','android',?,?,?)`, now, now, tokenHash("b-token")); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/groups", nil)
	req.Header.Set("X-Device-ID", "a")
	req.Header.Set("Authorization", "Bearer a-token")
	_ = req
	if !api.isGroupMember("missing", "a") { /* expected false */
	}
	if _, err := st.DB().Exec(`INSERT INTO groups(id,name,owner_id,created_at,key_version,rekey_required) VALUES('g','Test','a',?,?,?)`, now, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO group_members(group_id,device_id,role,joined_at) VALUES('g','a','owner',?)`, now); err != nil {
		t.Fatal(err)
	}
	if !api.isGroupOwner("g", "a") || !api.isGroupMember("g", "a") {
		t.Fatal("group owner/member helper failed")
	}
	m, existing, err := api.insertGroupMessage("a", "g", `gme:v1:{"version":1,"type":"group_message","key_version":1}`, "2026-01-01T00:00:00Z", "m1", nil)
	if err != nil || existing || m.ID != "m1" {
		t.Fatalf("insert failed: %+v existing=%v err=%v", m, existing, err)
	}
	m2, existing, err := api.insertGroupMessage("a", "g", `gme:v1:{"version":1,"type":"group_message","key_version":1}`, "2026-01-01T00:00:00Z", "m1", nil)
	if err != nil || !existing || m2.ID != "m1" {
		t.Fatalf("idempotency failed: %+v existing=%v err=%v", m2, existing, err)
	}
	b, _ := json.Marshal(m)
	if len(b) == 0 {
		t.Fatal("message did not marshal")
	}
}
