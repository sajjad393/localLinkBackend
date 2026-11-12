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

// Bundle 216: the admin lists carry names and photos (never file names).

type setup216 struct {
	api   *API
	token string
	ali   AuthResponse
	bilal AuthResponse
}

func newSetup216(t *testing.T) *setup216 {
	t.Helper()
	t.Setenv("LOCALLINK_FILES_DIR", t.TempDir()) // photos stay out of the repo
	_, api := newTestAPI(t)
	if err := api.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	s := &setup216{api: api}
	s.token = adminLogin211(t, api, "10.21.6.1").Token
	s.ali = registerDirectoryUser(t, api, "ali216", "dev-ali-216")
	s.bilal = registerDirectoryUser(t, api, "bilal216", "dev-bilal-216")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Ali has a name and a photo; Bilal has neither.
	dir := filepath.Join(api.fileRoot, "avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ali.jpg"), []byte("JPEGDATA216"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := api.store.DB().Exec(`INSERT OR REPLACE INTO profiles(user_id,display_name,avatar_path,avatar_content_type,avatar_size,avatar_sha256,avatar_updated_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		s.ali.Account.ID, "Ali Raza", "avatars/ali.jpg", "image/jpeg", 11, "abc", now, now, now); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *setup216) get(t *testing.T, h http.HandlerFunc, path string) map[string]any {
	t.Helper()
	res := doJSON(t, h, http.MethodGet, path, nil, map[string]string{"Authorization": "Bearer " + s.token})
	if res.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", path, res.Code, res.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func findRow216(t *testing.T, out map[string]any, list, key, value string) map[string]any {
	t.Helper()
	rows, _ := out[list].([]any)
	for _, r := range rows {
		m := r.(map[string]any)
		if m[key] == value {
			return m
		}
	}
	t.Fatalf("no %s row with %s=%s in %v", list, key, value, out)
	return nil
}

func TestAdminUsersShowNamePhotoAndLastSeen216(t *testing.T) {
	s := newSetup216(t)
	// No profile row at all: the name falls back to the username.
	if _, err := s.api.store.DB().Exec(`DELETE FROM profiles WHERE user_id=?`, s.bilal.Account.ID); err != nil {
		t.Fatal(err)
	}
	out := s.get(t, s.api.AdminUsers, "/api/v1/admin/users")
	ali := findRow216(t, out, "users", "id", s.ali.Account.ID)
	if ali["display_name"] != "Ali Raza" || ali["has_avatar"] != true {
		t.Fatalf("ali row = %v", ali)
	}
	if ali["last_seen_at"] == nil || ali["last_seen_at"] == "" {
		t.Fatalf("ali last seen missing: %v", ali)
	}
	bilal := findRow216(t, out, "users", "id", s.bilal.Account.ID)
	if bilal["display_name"] != "bilal216" || bilal["has_avatar"] != false {
		t.Fatalf("bilal row must fall back to the username: %v", bilal)
	}
}

func TestAdminDevicesShowOwnerNameAndBuild216(t *testing.T) {
	s := newSetup216(t)
	if _, err := s.api.store.DB().Exec(`INSERT OR REPLACE INTO device_builds(device_id,build,seen_at) VALUES(?,?,?)`, "dev-ali-216", 128, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	out := s.get(t, s.api.AdminDevices, "/api/v1/admin/devices")
	d := findRow216(t, out, "devices", "id", "dev-ali-216")
	if d["display_name"] != "Ali Raza" || d["has_avatar"] != true || d["app_build"] != float64(128) {
		t.Fatalf("device row = %v", d)
	}
}

func TestAdminUserAvatarOnlyForAdmins216(t *testing.T) {
	s := newSetup216(t)
	call := func(id string, headers map[string]string) (int, string) {
		req := doJSONReq216(t, http.MethodGet, "/api/v1/admin/users/"+id+"/avatar", id, headers)
		res := recorder216(s.api.AdminUserAvatar, req)
		return res.Code, res.Body.String()
	}
	if code, body := call(s.ali.Account.ID, map[string]string{"Authorization": "Bearer " + s.token}); code != http.StatusOK || body != "JPEGDATA216" {
		t.Fatalf("admin avatar = %d %q", code, body)
	}
	if code, _ := call(s.bilal.Account.ID, map[string]string{"Authorization": "Bearer " + s.token}); code != http.StatusNotFound {
		t.Fatalf("no photo must be 404, got %d", code)
	}
	if code, _ := call(s.ali.Account.ID, directoryHeaders(s.bilal)); code == http.StatusOK {
		t.Fatal("a normal user token must not read the admin avatar route")
	}
	if code, _ := call(s.ali.Account.ID, nil); code == http.StatusOK {
		t.Fatal("no token must not read the admin avatar route")
	}
}

func TestAdminCallsSecurityAndLogsShowNames216(t *testing.T) {
	s := newSetup216(t)
	db := s.api.store.DB()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO calls(id,caller_id,callee_id,status,started_at,duration_seconds,end_reason,last_activity_at) VALUES('call216','dev-ali-216','dev-bilal-216','ended',?,62,'hangup',?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO security_audit(action,user_id,device_id,source_hash,detail,created_at) VALUES('login_failed',?,?,'h','',?)`, s.ali.Account.ID, "dev-ali-216", now); err != nil {
		t.Fatal(err)
	}
	var adminID string
	if err := db.QueryRow(`SELECT id FROM admin_accounts WHERE username='test-admin'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO admin_audit(admin_id,action,target_id,detail,created_at) VALUES(?,?,?,?,?),(?,?,?,?,?)`,
		adminID, "user_block", s.ali.Account.ID, "", now,
		adminID, "network_rule", "dev-ali-216|dev-bilal-216", "", now); err != nil {
		t.Fatal(err)
	}

	calls := s.get(t, s.api.AdminCalls, "/api/v1/admin/calls")
	c := findRow216(t, calls, "calls", "id", "call216")
	if c["caller_name"] != "Ali Raza" || c["callee_name"] != "bilal216" /* Bundle 276: username, not the phone name */ || c["caller_user_id"] != s.ali.Account.ID {
		t.Fatalf("call row = %v", c)
	}
	sec := s.get(t, s.api.AdminSecurityEvents, "/api/v1/admin/security-events")
	e := findRow216(t, sec, "security_events", "action", "login_failed")
	if e["display_name"] != "Ali Raza" || e["username"] != "ali216" || e["device_name"] != "dev-ali-216" {
		t.Fatalf("security row = %v", e)
	}
	logs := s.get(t, s.api.AdminLogs, "/api/v1/admin/logs")
	l := findRow216(t, logs, "logs", "action", "user_block")
	if l["admin_name"] != "test-admin" || l["target_name"] != "Ali Raza" {
		t.Fatalf("log row = %v", l)
	}
	pair := findRow216(t, logs, "logs", "action", "network_rule")
	if pair["target_name"] != "dev-ali-216 \u2194 dev-bilal-216" && pair["target_name"] != "Ali Raza \u2194 bilal216" {
		t.Fatalf("pair target = %v", pair)
	}
}

func TestAdminTransfersKindNeverFileName216(t *testing.T) {
	for ct, want := range map[string]string{"image/jpeg": "photo", "video/mp4": "video", "audio/ogg": "voice", "application/pdf": "document", "": "document"} {
		if got := transferKind216(ct); got != want {
			t.Fatalf("kind(%q)=%q want %q", ct, got, want)
		}
	}
	raw, _ := json.Marshal(AdminTransfer{ID: "x", Kind: "photo", OwnerName: "Ali"})
	if strings.Contains(string(raw), "original_name") || strings.Contains(string(raw), "file_name") {
		t.Fatalf("transfers must never carry a file name: %s", raw)
	}
	s := newSetup216(t)
	out := s.get(t, s.api.AdminTransfers, "/api/v1/admin/transfers")
	body, _ := json.Marshal(out)
	if strings.Contains(string(body), "original_name") {
		t.Fatalf("transfer list leaked a file name: %s", body)
	}
}

func TestAdminSettingsShowBackupAndLimits216(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALLINK_BACKUP_DIR", dir)
	t.Setenv("LOCALLINK_TLS_CERT", "")
	t.Setenv("LOCALLINK_TLS_KEY", "")
	if err := os.WriteFile(filepath.Join(dir, "locallink-1.db"), make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newSetup216(t)
	out := s.get(t, s.api.AdminGetSettings, "/api/v1/admin/settings")
	if out["last_backup_at"] == nil || out["last_backup_bytes"] != float64(2048) {
		t.Fatalf("backup fields = %v", out)
	}
	if v, _ := out["max_file_bytes"].(float64); v <= 0 {
		t.Fatalf("max_file_bytes = %v", out)
	}
	if out["tls_enabled"] != false {
		t.Fatalf("tls must be off without cert+key: %v", out)
	}
}

func TestAdminFontsServedFromSameServer216(t *testing.T) {
	_, api := newTestAPI(t)
	for _, name := range []string{"Roboto-Regular.ttf", "Roboto-Medium.ttf", "Roboto-SemiBold.ttf", "Roboto-Bold.ttf"} {
		res := doJSON(t, api.AdminWeb, http.MethodGet, "/admin/fonts/"+name, nil)
		if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "font/ttf" || res.Body.Len() < 10000 {
			t.Fatalf("%s: %d %s %d", name, res.Code, res.Header().Get("Content-Type"), res.Body.Len())
		}
	}
	if res := doJSON(t, api.AdminWeb, http.MethodGet, "/admin/fonts/nope.ttf", nil); res.Code != http.StatusNotFound {
		t.Fatalf("missing font must 404, got %d", res.Code)
	}
}
