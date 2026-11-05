package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Bundle 221 (a): the server is a postman; conv_index keeps the chat list.

type pm221 struct {
	t   *testing.T
	api *API
	now time.Time
	dir string
}

func newPM221(t *testing.T) *pm221 {
	_, api := newTestAPI(t)
	p := &pm221{t: t, api: api, now: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), dir: t.TempDir()}
	ts := postmanTime(p.now.Add(-200 * 24 * time.Hour))
	for _, u := range []string{"user-a", "user-b", "user-c"} {
		p.exec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at) VALUES(?,?,'s','h',1,'active',?,?)`, u, u, ts, ts)
	}
	for _, d := range [][2]string{{"dev-a", "user-a"}, {"dev-b", "user-b"}, {"dev-c", "user-c"}} {
		p.exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,status,user_id) VALUES(?,?,?,?,?,'active',?)`, d[0], d[0], "android", ts, ts, d[1])
	}
	p.exec(`INSERT INTO groups(id,name,owner_id,created_at) VALUES('g1','Site team','dev-a',?)`, ts)
	for _, d := range []string{"dev-a", "dev-b", "dev-c"} {
		p.exec(`INSERT INTO group_members(group_id,device_id,role,joined_at) VALUES('g1',?,'member',?)`, d, ts)
	}
	return p
}

func (p *pm221) exec(q string, args ...any) {
	p.t.Helper()
	if _, err := p.api.store.DB().Exec(q, args...); err != nil {
		p.t.Fatalf("%v\n%s", err, q)
	}
}

func (p *pm221) ago(days float64) string {
	return postmanTime(p.now.Add(-time.Duration(days * float64(24*time.Hour))))
}

func (p *pm221) direct(id string, createdDays float64, deliveredDays float64) {
	delivered := any(nil)
	if deliveredDays >= 0 {
		delivered = p.ago(deliveredDays)
	}
	p.exec(`INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status,delivered_at,server_seq) VALUES(?,?,?,?,?,'sent',?,(SELECT COALESCE(MAX(server_seq),0)+1 FROM messages))`,
		id, "dev-a", "dev-b", "secret", p.ago(createdDays), delivered)
	p.exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES('dev-b','message',?)`, id)
}

func (p *pm221) group(id string, createdDays float64) {
	p.exec(`INSERT INTO group_messages(id,group_id,sender_id,body,created_at,server_seq) VALUES(?,'g1','dev-a','hi',?,(SELECT COALESCE(MAX(server_seq),0)+1 FROM group_messages))`, id, p.ago(createdDays))
	for _, d := range []string{"dev-a", "dev-b", "dev-c"} {
		p.exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES(?,'group_message',?)`, d, id)
	}
}

func (p *pm221) receipt(id, device string, deliveredDays float64) {
	p.exec(`INSERT INTO group_receipts(message_id,group_id,device_id,sender_id,delivered_at,read_at,updated_at) VALUES(?,'g1',?,'dev-a',?,'',?)`, id, device, p.ago(deliveredDays), p.ago(deliveredDays))
}

func (p *pm221) file(id string) string {
	path := filepath.Join(p.dir, id+".bin")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		p.t.Fatal(err)
	}
	p.exec(`INSERT INTO files(id,owner_id,original_name,content_type,size,sha256,storage_path,thumbnail_path,width,height,is_image,created_at,status,storage_size,thumbnail_storage_size) VALUES(?,'dev-a','a.bin','application/octet-stream',4,'sha',?,'',0,0,0,?,'ready',4,0)`, id, path, p.ago(100))
	return path
}

func (p *pm221) count(q string, args ...any) int {
	p.t.Helper()
	var n int
	if err := p.api.store.DB().QueryRow(q, args...).Scan(&n); err != nil {
		p.t.Fatal(err)
	}
	return n
}

func (p *pm221) has(table, id string) bool {
	return p.count(`SELECT COUNT(*) FROM `+table+` WHERE id=?`, id) == 1
}

func (p *pm221) clean() PostmanResult {
	p.t.Helper()
	res, err := p.api.CleanupMessages221(p.now)
	if err != nil {
		p.t.Fatal(err)
	}
	return res
}

func TestPostmanDirectRules221(t *testing.T) {
	p := newPM221(t)
	p.direct("d-6", 10, 6)   // delivered 6 days ago: kept
	p.direct("d-8", 10, 8)   // delivered 8 days ago: gone
	p.direct("u-59", 59, -1) // not delivered, 59 days: kept
	p.direct("u-61", 61, -1) // not delivered, 61 days: gone
	res := p.clean()
	if res.Direct != 2 {
		t.Fatalf("direct removed = %d, want 2", res.Direct)
	}
	for id, want := range map[string]bool{"d-6": true, "d-8": false, "u-59": true, "u-61": false} {
		if p.has("messages", id) != want {
			t.Fatalf("%s kept=%v want %v", id, !want, want)
		}
	}
	if n := p.count(`SELECT COUNT(*) FROM sync_events WHERE event_id IN ('d-8','u-61')`); n != 0 {
		t.Fatalf("sync events left: %d", n)
	}
	// Second run: nothing more.
	if res := p.clean(); res.Direct != 0 {
		t.Fatalf("second run removed %d", res.Direct)
	}
}

func TestPostmanGroupRules221(t *testing.T) {
	p := newPM221(t)
	p.group("g-missing", 30) // dev-c never got it: kept (< 60 days)
	p.receipt("g-missing", "dev-b", 20)
	p.group("g-all-8", 30) // both delivered, last 8 days ago: gone
	p.receipt("g-all-8", "dev-b", 20)
	p.receipt("g-all-8", "dev-c", 8)
	p.group("g-all-5", 30) // last receipt 5 days ago: kept
	p.receipt("g-all-5", "dev-b", 20)
	p.receipt("g-all-5", "dev-c", 5)
	p.group("g-61", 61) // too old anyway
	res := p.clean()
	if res.Group != 2 {
		t.Fatalf("group removed = %d, want 2", res.Group)
	}
	for id, want := range map[string]bool{"g-missing": true, "g-all-8": false, "g-all-5": true, "g-61": false} {
		if p.has("group_messages", id) != want {
			t.Fatalf("%s kept=%v want %v", id, !want, want)
		}
	}
	if n := p.count(`SELECT COUNT(*) FROM group_receipts WHERE message_id='g-all-8'`); n != 0 {
		t.Fatalf("receipts left: %d", n)
	}
}

func TestPostmanFiles221(t *testing.T) {
	p := newPM221(t)
	only := p.file("f-only")
	shared := p.file("f-shared")
	p.direct("old", 10, 9)
	p.direct("new", 3, 2)
	p.exec(`INSERT INTO message_attachments(message_id,file_id,position) VALUES('old','f-only',0),('old','f-shared',1),('new','f-shared',0)`)
	res := p.clean()
	if res.Files != 1 {
		t.Fatalf("files removed = %d, want 1", res.Files)
	}
	if _, err := os.Stat(only); !os.IsNotExist(err) {
		t.Fatal("file used only by the removed message is still on disk")
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatal("shared file was removed")
	}
	if !p.has("files", "f-shared") || p.has("files", "f-only") {
		t.Fatal("files rows wrong")
	}
}

func TestPostmanConvIndex221(t *testing.T) {
	p := newPM221(t)
	p.direct("m1", 20, 19)
	p.direct("m2", 10, 9)
	p.group("g1m", 20)
	var count int
	var first, last string
	if err := p.api.store.DB().QueryRow(`SELECT count, first_at, last_at FROM conv_index WHERE kind='direct' AND a='dev-a' AND b='dev-b'`).Scan(&count, &first, &last); err != nil {
		t.Fatal(err)
	}
	if count != 2 || first != p.ago(20) || last != p.ago(10) {
		t.Fatalf("conv_index direct = %d %s %s", count, first, last)
	}
	p.clean()
	if p.count(`SELECT COUNT(*) FROM messages`) != 0 {
		t.Fatal("messages should be gone")
	}
	// The chat list survives the clean-up (count of all time, 0 on server).
	if n := p.count(`SELECT count FROM conv_index WHERE kind='direct' AND a='dev-a' AND b='dev-b'`); n != 2 {
		t.Fatalf("conv_index count after clean-up = %d", n)
	}
	if n := p.count(`SELECT count FROM conv_index WHERE kind='group' AND group_id='g1'`); n != 1 {
		t.Fatalf("group conv_index = %d", n)
	}
	// Reverse direction counts in the same row.
	p.exec(`INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status,server_seq) VALUES('m3','dev-b','dev-a','x',?,'sent',99)`, p.ago(1))
	if n := p.count(`SELECT count FROM conv_index WHERE kind='direct' AND a='dev-a' AND b='dev-b'`); n != 3 {
		t.Fatalf("reverse message count = %d", n)
	}
}

func TestPostmanSyncSkipsCleaned221(t *testing.T) {
	p := newPM221(t)
	p.direct("gone", 30, 20)
	p.direct("kept", 2, 1)
	p.clean()
	if p.count(`SELECT COUNT(*) FROM sync_events WHERE device_id='dev-b' AND event_type='message'`) != 1 {
		t.Fatal("only the kept message should have a sync event")
	}
	// A stale event for a removed row (older server) is skipped by sync.
	p.exec(`INSERT INTO sync_events(device_id,event_type,event_id) VALUES('dev-b','message','gone')`)
	if p.has("messages", "gone") {
		t.Fatal("gone must be gone")
	}
}

func TestPostmanVacuumRule221(t *testing.T) {
	p := newPM221(t)
	night := time.Date(2026, 11, 6, 2, 0, 0, 0, time.Local)
	if p.api.postmanShouldVacuum(night) {
		t.Fatal("no VACUUM before 03:00")
	}
	morning := time.Date(2026, 11, 6, 4, 0, 0, 0, time.Local)
	// Make free pages: fill and delete.
	big := make([]byte, 64<<10)
	for i := 0; i < 40; i++ {
		p.exec(`INSERT INTO admin_settings(key,value,updated_at) VALUES(?,?,?)`, "pad"+string(rune('A'+i)), string(big), "x")
	}
	p.exec(`DELETE FROM admin_settings WHERE key LIKE 'pad%'`)
	if !p.api.postmanShouldVacuum(morning) {
		total, free := p.api.postmanDBBytes()
		t.Fatalf("VACUUM expected (total=%d free=%d)", total, free)
	}
	_, did, err := p.api.PostmanTick(morning)
	if err != nil || !did {
		t.Fatalf("tick vacuum did=%v err=%v", did, err)
	}
	if _, free := p.api.postmanDBBytes(); free != 0 {
		t.Fatalf("free after VACUUM = %d", free)
	}
	// Once a day.
	if p.api.postmanShouldVacuum(morning.Add(time.Hour)) {
		t.Fatal("second VACUUM on the same day")
	}
}

func TestAdminConversationsFromIndex221(t *testing.T) {
	t.Setenv("LOCALLINK_ENV", "test")
	t.Setenv("LOCALLINK_ADMIN_TOKEN", "test-admin-token")
	p := newPM221(t)
	p.direct("m1", 20, 19)
	p.direct("m2", 1, 0.5)
	p.group("g1m", 70)
	p.clean()
	res := adminGet(t, p.api.AdminConversations, "/api/v1/admin/conversations", "")
	if res.Code != http.StatusOK {
		t.Fatalf("status %d %s", res.Code, res.Body.String())
	}
	var out struct {
		Conversations []conversationSummary `json:"conversations"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Conversations) != 2 {
		t.Fatalf("conversations = %d: %s", len(out.Conversations), res.Body.String())
	}
	for _, c := range out.Conversations {
		switch c.Kind {
		case "direct":
			if c.MessageCount != 2 || c.OnServer != 1 {
				t.Fatalf("direct count=%d on_server=%d", c.MessageCount, c.OnServer)
			}
		case "group":
			if c.MessageCount != 1 || c.OnServer != 0 || c.GroupName != "Site team" {
				t.Fatalf("group %+v", c)
			}
		}
	}
	if strings.Contains(res.Body.String(), "secret") {
		t.Fatal("list carries text")
	}
}
