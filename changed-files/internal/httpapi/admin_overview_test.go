package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func adminHeaders() map[string]string {
	return map[string]string{"X-Admin-Token": "test-admin-token"}
}

func setupAdminTest(t *testing.T) (*API, func()) {
	t.Helper()
	t.Setenv("LOCALLINK_ENV", "test")
	t.Setenv("LOCALLINK_ADMIN_TOKEN", "test-admin-token")
	st, api := newTestAPI(t)
	return api, func() { _ = st.Close() }
}

func seedOverviewData(t *testing.T, api *API) {
	t.Helper()
	db := api.store.DB()
	now := time.Now().UTC()
	hour := func(offset int) string { return now.Add(time.Duration(offset) * time.Hour).Format(time.RFC3339Nano) }

	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	mustExec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at,failed_login_count)
		VALUES('u1','alice','s','h',1,'active',?,?,0)`, hour(-3), hour(-3))
	mustExec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at,failed_login_count,locked_until)
		VALUES('u2','bob','s','h',1,'active',?,?,3,?)`, hour(-50), hour(-50), now.Add(time.Hour).Format(time.RFC3339Nano))
	mustExec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,status)
		VALUES('d1','Pixel 8','android',?,?,'active')`, hour(-2), now.Format(time.RFC3339Nano))
	mustExec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,status,revoked_at)
		VALUES('d2','Nokia','android',?,?,'revoked',?)`, hour(-30), hour(-30), hour(-29))
	mustExec(`INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status)
		VALUES('m1','d1','d2','hello',?, 'sent'), ('m2','d1','d2','again',?,'sent')`, hour(-1), hour(-1))
	mustExec(`INSERT INTO calls(id,caller_id,callee_id,status,started_at)
		VALUES('c1','d1','d2','connected',?)`, hour(-1))
	mustExec(`INSERT INTO files(id,owner_id,original_name,content_type,size,sha256,storage_path,created_at,status)
		VALUES('f1','d1','photo.jpg','image/jpeg',2048,'abc','data/files/f1',?,'ready')`, hour(-2))
	mustExec(`INSERT INTO security_audit(action,user_id,device_id,source_hash,detail,created_at)
		VALUES('login_failed','u1','d1','x','bad password',?),
		       ('login_failed','u1','d1','x','bad password',?),
		       ('device_revoked','','d2','','revoked by admin',?),
		       ('login_success','u1','d1','','ok',?)`, hour(-1), hour(-2), hour(-3), hour(-4))
	mustExec(`INSERT INTO admin_audit(admin_id,action,target_id,detail,created_at)
		VALUES('a1','backup','','nightly',?)`, hour(-5))
}

func TestAdminAnalyticsBucketsAndTotals(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	seedOverviewData(t, api)

	res := doJSON(t, api.AdminAnalytics, http.MethodGet, "/api/v1/admin/analytics?range=24h", nil, adminHeaders())
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		OK            bool                       `json:"ok"`
		Range         string                     `json:"range"`
		BucketSeconds int64                      `json:"bucket_seconds"`
		Series        map[string][]map[string]any `json:"series"`
		Totals        map[string]int64           `json:"totals"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.OK || payload.Range != "24h" || payload.BucketSeconds != 3600 {
		t.Fatalf("unexpected envelope: %+v", payload)
	}
	if payload.Totals["messages"] != 2 {
		t.Fatalf("messages total=%d want 2", payload.Totals["messages"])
	}
	if payload.Totals["users"] != 1 {
		t.Fatalf("users total=%d want 1 (one user inside the 24h window)", payload.Totals["users"])
	}
	if payload.Totals["files"] != 1 || payload.Totals["file_bytes"] != 2048 {
		t.Fatalf("file totals wrong: files=%d bytes=%d", payload.Totals["files"], payload.Totals["file_bytes"])
	}
	if payload.Totals["calls"] != 1 || payload.Totals["security_events"] != 4 {
		t.Fatalf("call/security totals wrong: %+v", payload.Totals)
	}
	messages, ok := payload.Series["messages"]
	if !ok || len(messages) == 0 {
		t.Fatal("messages series missing")
	}
	// Series must be complete and zero-filled: 25 boundary buckets cover a 24h
	// window (inclusive of both ends).
	if len(messages) < 24 || len(messages) > 26 {
		t.Fatalf("expected ~25 hourly buckets, got %d", len(messages))
	}
	var sum int64
	for _, point := range messages {
		value, _ := point["v"].(float64)
		sum += int64(value)
	}
	if sum != 2 {
		t.Fatalf("series sum=%d want 2", sum)
	}
}

func TestAdminAnalyticsRangeValidation(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()

	for _, rng := range []string{"24h", "7d", "30d", "90d", ""} {
		res := doJSON(t, api.AdminAnalytics, http.MethodGet, "/api/v1/admin/analytics?range="+rng, nil, adminHeaders())
		if res.Code != http.StatusOK {
			t.Fatalf("range=%q status=%d", rng, res.Code)
		}
	}
	res := doJSON(t, api.AdminAnalytics, http.MethodGet, "/api/v1/admin/analytics?range=5y", nil, adminHeaders())
	if res.Code != http.StatusBadRequest {
		t.Fatalf("invalid range should be rejected, got %d", res.Code)
	}
}

func TestAdminAnalyticsRequiresAdmin(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	res := doJSON(t, api.AdminAnalytics, http.MethodGet, "/api/v1/admin/analytics?range=24h", nil)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated analytics must be rejected, got %d", res.Code)
	}
}

func TestAdminHealthReportsRealComponents(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	seedOverviewData(t, api)

	res := doJSON(t, api.AdminHealth, http.MethodGet, "/api/v1/admin/health", nil, adminHeaders())
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		OK         bool `json:"ok"`
		Overall    string `json:"overall"`
		Components []struct {
			Key    string `json:"key"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"components"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.OK {
		t.Fatal("health not ok")
	}
	want := map[string]bool{
		"database": false, "cpu": false, "memory": false, "storage": false,
		"websocket": false, "lan_discovery": false, "mesh": false,
		"file_service": false, "call_service": false,
	}
	validStatus := map[string]bool{"healthy": true, "warning": true, "critical": true, "offline": true, "unknown": true}
	for _, component := range payload.Components {
		if _, ok := want[component.Key]; !ok {
			t.Fatalf("unexpected component %q", component.Key)
		}
		want[component.Key] = true
		if !validStatus[component.Status] {
			t.Fatalf("component %s has invalid status %q", component.Key, component.Status)
		}
	}
	for key, seen := range want {
		if !seen {
			t.Fatalf("component %q missing from health payload", key)
		}
	}
	if !validStatus[payload.Overall] {
		t.Fatalf("invalid overall status %q", payload.Overall)
	}
}

func TestAdminSecuritySummaryCountsAndSeverity(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	seedOverviewData(t, api)

	res := doJSON(t, api.AdminSecuritySummary, http.MethodGet, "/api/v1/admin/security-summary", nil, adminHeaders())
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Counts map[string]any `json:"counts"`
		Events []struct {
			Action   string `json:"action"`
			Severity string `json:"severity"`
		} `json:"recent_events"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	check := func(key string, want float64) {
		t.Helper()
		got, _ := payload.Counts[key].(float64)
		if got != want {
			t.Fatalf("counts[%s]=%v want %v", key, payload.Counts[key], want)
		}
	}
	check("failed_logins_24h", 2)
	check("critical_alerts_24h", 1)
	check("locked_users", 1)
	check("revoked_devices", 1)

	severities := map[string]string{}
	for _, event := range payload.Events {
		severities[event.Action] = event.Severity
	}
	if severities["device_revoked"] != "critical" {
		t.Fatalf("device_revoked severity=%q want critical", severities["device_revoked"])
	}
	if severities["login_failed"] != "warning" {
		t.Fatalf("login_failed severity=%q want warning", severities["login_failed"])
	}
	if severities["login_success"] != "info" {
		t.Fatalf("login_success severity=%q want info", severities["login_success"])
	}
}

func TestAdminActivityIsChronologicalAndLimited(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	seedOverviewData(t, api)

	res := doJSON(t, api.AdminActivity, http.MethodGet, "/api/v1/admin/activity?limit=5", nil, adminHeaders())
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Events []struct {
			Type      string `json:"type"`
			Severity  string `json:"severity"`
			Title     string `json:"title"`
			CreatedAt string `json:"created_at"`
		} `json:"events"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Events) != 5 {
		t.Fatalf("limit not honoured: got %d events", len(payload.Events))
	}
	for i := 1; i < len(payload.Events); i++ {
		if payload.Events[i-1].CreatedAt < payload.Events[i].CreatedAt {
			t.Fatalf("activity not newest-first at %d", i)
		}
	}
	kinds := map[string]bool{}
	for _, event := range payload.Events {
		kinds[event.Type] = true
		if event.Title == "" {
			t.Fatal("activity event without a title")
		}
	}
	if !kinds["file_uploaded"] && !kinds["messages"] {
		// The newest seeded events are the 1h-old message/call/file rows; at
		// least one operational type must surface in the top five.
		if !kinds["call_started"] && !kinds["file_uploaded"] && !kinds["security_event"] {
			t.Fatalf("expected operational event types, got %v", kinds)
		}
	}
}

func TestAdminActivityRejectsBadLimit(t *testing.T) {
	api, cleanup := setupAdminTest(t)
	defer cleanup()
	res := doJSON(t, api.AdminActivity, http.MethodGet, "/api/v1/admin/activity?limit=abc", nil, adminHeaders())
	if res.Code != http.StatusOK {
		t.Fatalf("bad limit should fall back to default, got %d", res.Code)
	}
}
