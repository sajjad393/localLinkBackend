package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"locallink/backend/internal/store"
)

func TestStatusReportsPhase40(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	register := doJSON(t, a.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": "status-device", "name": "Status Device", "platform": "android"}, nil)
	if register.Code != http.StatusCreated {
		t.Fatalf("registration: %d %s", register.Code, register.Body.String())
	}
	var registration struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(register.Body.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	res := doJSON(t, a.Status, http.MethodGet, "/api/v1/status", nil, map[string]string{
		"Authorization": "Bearer " + registration.Token,
		"X-Device-ID":   "status-device",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("status: %d %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("missing nosniff header: %q", got)
	}
	if got := res.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("missing no-store header: %q", got)
	}
	if !contains(res.Body.String(), `"version":"phase-l"`) {
		t.Fatalf("wrong version: %s", res.Body.String())
	}
}

func TestRegistrationRateLimiter(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	key := "10.0.0.5:1234"
	for i := 0; i < 5; i++ {
		if !a.allowRegistration(key) {
			t.Fatalf("attempt %d unexpectedly blocked", i+1)
		}
	}
	if a.allowRegistration(key) {
		t.Fatal("sixth attempt should be blocked")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestStatusRequiresDeviceAuth(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	api := New(st)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	api.Status(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRecoveryStatusRejectsURLQuerySecretAndRequiresPostBody(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	get := doJSON(t, a.AccountRecoveryStatus, http.MethodGet, "/api/v1/account/recovery/status?request_id=req&request_secret=secret", nil, nil)
	if get.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET recovery status should be rejected, got %d %s", get.Code, get.Body.String())
	}
	post := doJSON(t, a.AccountRecoveryStatus, http.MethodPost, "/api/v1/account/recovery/status", map[string]any{"request_id": "missing", "request_secret": "secret"}, nil)
	if post.Code != http.StatusNotFound {
		t.Fatalf("POST recovery status should process the body, got %d %s", post.Code, post.Body.String())
	}
}

func TestPersistentSecurityActionSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "locallink.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := New(st)
	for i := 0; i < 3; i++ {
		if !a.allowPersistentSecurityAction("test", "account-a", 3, time.Minute) {
			t.Fatalf("attempt %d unexpectedly blocked", i+1)
		}
	}
	if a.allowPersistentSecurityAction("test", "account-a", 3, time.Minute) {
		t.Fatal("fourth persistent attempt should be blocked")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a = New(st)
	if a.allowPersistentSecurityAction("test", "account-a", 3, time.Minute) {
		t.Fatal("persistent limiter was reset by process restart")
	}
}

func TestPersistentSecurityActionDoesNotStoreRawRateKey(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	const sensitive = "user@example.local"
	if !a.allowPersistentSecurityAction("test", sensitive, 2, time.Minute) {
		t.Fatal("first attempt unexpectedly blocked")
	}
	var stored string
	if err := st.DB().QueryRow(`SELECT rate_key FROM security_rate_limits WHERE scope='test' LIMIT 1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == sensitive || contains(stored, sensitive) {
		t.Fatalf("raw sensitive limiter key persisted: %q", stored)
	}
	if len(stored) != 64 {
		t.Fatalf("hashed limiter key length = %d, want 64", len(stored))
	}
}

func TestAttachmentStorageQuotaIncludesThumbnailBytesInQueries(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.DB().Exec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at) VALUES('u','quotauser','s','h',1,'active','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,user_id) VALUES('d','Quota','android','now','now','u')`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO files(id,owner_id,original_name,content_type,size,sha256,storage_path,thumbnail_path,created_at,status,storage_size,thumbnail_storage_size) VALUES('f','d','x.png','image/png',100,'x','/tmp/x','/tmp/t','now','ready',100,25)`); err != nil {
		t.Fatal(err)
	}
	a := New(st)
	if got := a.deviceStorageUsed("d"); got != 125 {
		t.Fatalf("deviceStorageUsed = %d, want 125", got)
	}
}
