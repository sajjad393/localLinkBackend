package httpapi

// Bundle 322 (W4 / V1 + V5 + V7): the App version page picks the build from
// what is on the server. Old callers must see exactly the 321 behaviour.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func jsonUnmarshal322(b []byte, v any) error { return json.Unmarshal(b, v) }

// numOf322 reads a number out of a decoded JSON map (float64 or json.Number).
func numOf322(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}

// list322 calls the admin GET and returns the decoded body.
func list322(t *testing.T, e apkEnv299) map[string]any {
	t.Helper()
	rec := e.get(t, e.api.AdminGetVersionRule, "/api/v1/admin/version-rule", "", map[string]string{
		"Authorization": "Bearer " + e.token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET version-rule %d %s", rec.Code, rec.Body.String())
	}
	out := map[string]any{}
	if err := jsonUnmarshal322(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body: %v", err)
	}
	return out
}

func TestVersionRule322ReturnsUploads(t *testing.T) {
	e := newAPKEnv299(t)
	key := cert299(t, "sajjad")
	if code, out := e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 183, "3.0.112", key)); code != http.StatusOK {
		t.Fatalf("upload 183: %d %v", code, out)
	}
	if code, out := e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 184, "3.0.113", key)); code != http.StatusOK {
		t.Fatalf("upload 184: %d %v", code, out)
	}
	// The rule is behind the newest upload - the case the page must show.
	setRule213(t, e.api, e.token, map[string]any{"min_build": 183, "min_version": "3.0.112", "mode": "warn"})

	body := list322(t, e)
	// 321 keys are untouched.
	if body["rule"] == nil || body["builds"] == nil {
		t.Fatalf("321 keys missing: %v", body)
	}
	uploads, ok := body["uploads"].([]any)
	if !ok || len(uploads) != 2 {
		t.Fatalf("uploads: %T %v", body["uploads"], body["uploads"])
	}
	newest := uploads[0].(map[string]any)
	if numOf322(newest["build"]) != 184 || newest["newest"] != true {
		t.Fatalf("newest must be build 184: %v", newest)
	}
	if newest["is_rule"] != false {
		t.Fatalf("184 is not the rule: %v", newest)
	}
	if newest["on_disk"] != true {
		t.Fatalf("the file is on the server: %v", newest)
	}
	old := uploads[1].(map[string]any)
	if numOf322(old["build"]) != 183 || old["is_rule"] != true {
		t.Fatalf("183 is the rule: %v", old)
	}
	if top, ok := body["newest_upload"].(map[string]any); !ok || numOf322(top["build"]) != 184 {
		t.Fatalf("newest_upload: %v", body["newest_upload"])
	}
}

func TestVersionRule322NoUploadsKeepsTheOldShape(t *testing.T) {
	e := newAPKEnv299(t)
	body := list322(t, e)
	uploads, ok := body["uploads"].([]any)
	if !ok || len(uploads) != 0 {
		t.Fatalf("no uploads expected: %v", body["uploads"])
	}
	if body["newest_upload"] != nil {
		t.Fatalf("newest_upload must be nil: %v", body["newest_upload"])
	}
	// A rule can still be saved with no APK at all (typed by hand).
	setRule213(t, e.api, e.token, map[string]any{"min_build": 150, "min_version": "3.0.90", "mode": "warn"})
	if body := list322(t, e); body["rule"] == nil {
		t.Fatalf("the rule was not saved: %v", body)
	}
}

func TestVersionRule322RefusesABuildThatIsNotOnTheServer(t *testing.T) {
	e := newAPKEnv299(t)
	key := cert299(t, "sajjad")
	if code, out := e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 184, "3.0.113", key)); code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, out)
	}
	// The page asks for the guard: a build nobody can download is refused.
	res := doJSON(t, e.api.AdminSetVersionRule, http.MethodPost, "/api/v1/admin/version-rule",
		map[string]any{"min_build": 999, "min_version": "9.9.9", "mode": "warn", "require_upload322": true},
		map[string]string{"Authorization": "Bearer " + e.token})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a build not on the server, got %d %s", res.Code, res.Body.String())
	}
	// A build that IS on the server is accepted.
	setRule213(t, e.api, e.token, map[string]any{"min_build": 184, "min_version": "3.0.113", "mode": "warn", "require_upload322": true})
	// And a caller that does not send the flag keeps the 321 behaviour.
	setRule213(t, e.api, e.token, map[string]any{"min_build": 999, "min_version": "9.9.9", "mode": "warn"})
}
