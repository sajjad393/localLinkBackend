package httpapi

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Bundle 213: minimum app version (signed rule, gate, web endpoints).

func gate213(api *API) http.Handler {
	return api.VersionGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func hit213(h http.Handler, path, build string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if build != "" {
		req.Header.Set(versionBuildHeader, build)
		req.Header.Set("X-Device-ID", "dev-gate-213")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func setRule213(t *testing.T, api *API, token string, body map[string]any) {
	t.Helper()
	res := doJSON(t, api.AdminSetVersionRule, http.MethodPost, "/api/v1/admin/version-rule", body,
		map[string]string{"Authorization": "Bearer " + token})
	if res.Code != http.StatusOK {
		t.Fatalf("set rule %d %s", res.Code, res.Body.String())
	}
}

func TestVersionGateNoRulePassesEveryone(t *testing.T) {
	_, api := newTestAPI(t)
	h := gate213(api)
	if c := hit213(h, "/api/v1/messages", ""); c != http.StatusOK {
		t.Fatalf("no rule, no header: %d", c)
	}
	if c := hit213(h, "/api/v1/messages", "100"); c != http.StatusOK {
		t.Fatalf("no rule, old build: %d", c)
	}
}

func TestVersionGateStopAndVeryOld(t *testing.T) {
	_, api := newTestAPI(t)
	if err := api.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	admin := adminLogin211(t, api, "10.21.3.1")
	h := gate213(api)

	// Warn only: nobody refused.
	setRule213(t, api, admin.Token, map[string]any{"min_build": 125, "min_version": "3.0.54", "mode": "warn"})
	if c := hit213(h, "/api/v1/messages", "124"); c != http.StatusOK {
		t.Fatalf("warn only must pass: %d", c)
	}
	// Stop after 3 days: still passes now.
	setRule213(t, api, admin.Token, map[string]any{"min_build": 125, "min_version": "3.0.54", "mode": "stop", "stop_days": 3})
	if c := hit213(h, "/api/v1/messages", "124"); c != http.StatusOK {
		t.Fatalf("stop later must pass now: %d", c)
	}
	// Stop now: old build refused, new build passes, no header passes (switch off).
	setRule213(t, api, admin.Token, map[string]any{"min_build": 125, "min_version": "3.0.54", "mode": "stop", "stop_days": 0})
	if c := hit213(h, "/api/v1/messages", "124"); c != http.StatusUpgradeRequired {
		t.Fatalf("stopped build: %d", c)
	}
	if c := hit213(h, "/ws", "124"); c != http.StatusUpgradeRequired {
		t.Fatalf("socket must be gated: %d", c)
	}
	if c := hit213(h, "/api/v1/messages", "125"); c != http.StatusOK {
		t.Fatalf("new build: %d", c)
	}
	if c := hit213(h, "/api/v1/messages", ""); c != http.StatusOK {
		t.Fatalf("very old with switch off: %d", c)
	}
	// Never gated.
	for _, p := range []string{"/api/v1/admin/summary", "/admin", "/health", "/api/v1/trust", "/api/v1/version-rule"} {
		if c := hit213(h, p, "124"); c != http.StatusOK {
			t.Fatalf("%s must not be gated: %d", p, c)
		}
	}
	// Very old switch on: no header refused.
	setRule213(t, api, admin.Token, map[string]any{"min_build": 125, "min_version": "3.0.54", "mode": "warn", "very_old": true})
	if c := hit213(h, "/api/v1/messages", ""); c != http.StatusUpgradeRequired {
		t.Fatalf("very old with switch on: %d", c)
	}
	// Remove: everyone back.
	setRule213(t, api, admin.Token, map[string]any{"remove": true})
	if c := hit213(h, "/api/v1/messages", ""); c != http.StatusOK {
		t.Fatalf("after remove: %d", c)
	}
}

func TestVersionRuleServerSignedAndNewestWins(t *testing.T) {
	s := setup212(t)
	api := s.api
	setRule213(t, api, s.admin.Token, map[string]any{"min_build": 125, "min_version": "3.0.54", "mode": "warn"})
	res := doJSON(t, api.VersionRule, http.MethodGet, "/api/v1/version-rule", nil)
	var out struct {
		Rule *signedBlob `json:"rule"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if out.Rule == nil || !api.serverVerify(*out.Rule) {
		t.Fatal("rule must be server signed")
	}
	// An admin phone rule made offline (seq 2) arrives by sync and wins.
	cert := s.cert
	rec := versionRuleBody{V: 1, Kind: "version_rule", ID: uuid.NewString(), MinBuild: 126, MinVersion: "3.0.55",
		Mode: "stop", StopAt: time.Now().UTC().Format(time.RFC3339Nano), Seq: 2,
		At: time.Now().UTC().Format(time.RFC3339Nano), By: "test-admin", Signer: "admin", Cert: &cert}
	raw, _ := json.Marshal(rec)
	blob := signedBlob{Body: string(raw), Sig: b64url(ed25519.Sign(s.phoneKey, raw))}
	headers := map[string]string{"X-Device-ID": "dev-ali-212", "Authorization": "Bearer " + s.ali.Token}
	res = doJSON(t, api.VersionRuleSync, http.MethodPost, "/api/v1/version-rule/sync", map[string]any{"rule": blob}, headers)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"applied":true`) {
		t.Fatalf("sync %d %s", res.Code, res.Body.String())
	}
	cur, _ := api.currentVersionRule()
	if cur == nil || cur.MinBuild != 126 {
		t.Fatalf("newest must win: %+v", cur)
	}
	// The older one again: ignored.
	res = doJSON(t, api.VersionRuleSync, http.MethodPost, "/api/v1/version-rule/sync", map[string]any{"rule": out.Rule}, headers)
	if !strings.Contains(res.Body.String(), `"applied":false`) {
		t.Fatalf("older must be ignored: %s", res.Body.String())
	}
	// A changed rule is rejected.
	bad := signedBlob{Body: strings.Replace(blob.Body, `"min_build":126`, `"min_build":999`, 1), Sig: blob.Sig}
	if _, why := api.checkVersionRule(bad); why != "bad_sig" {
		t.Fatalf("changed rule: %q", why)
	}
}

func TestVersionRuleOwnerOnlyAndBuildsList(t *testing.T) {
	_, api := newTestAPI(t)
	if err := api.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	admin := adminLogin211(t, api, "10.21.3.2")
	res := doJSON(t, api.AdminSetVersionRule, http.MethodPost, "/api/v1/admin/version-rule",
		map[string]any{"min_build": 0, "min_version": "", "mode": "warn"},
		map[string]string{"Authorization": "Bearer " + admin.Token})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("bad version must be refused: %d", res.Code)
	}
	res = doJSON(t, api.AdminSetVersionRule, http.MethodPost, "/api/v1/admin/version-rule",
		map[string]any{"min_build": 125, "min_version": "3.0.54", "mode": "warn"}, nil)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("no admin: %d", res.Code)
	}
	// Builds the phones sent are listed.
	hit213(gate213(api), "/api/v1/messages", "124")
	res = doJSON(t, api.AdminGetVersionRule, http.MethodGet, "/api/v1/admin/version-rule", nil,
		map[string]string{"Authorization": "Bearer " + admin.Token})
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"build":124`) {
		t.Fatalf("builds %d %s", res.Code, res.Body.String())
	}
}

func TestVersionRuleWebPageCallsRegisteredRoutes(t *testing.T) {
	routes, err := os.ReadFile(filepath.Join("..", "..", "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"api.VersionGate(mux)", `"GET /api/v1/version-rule"`, `"POST /api/v1/version-rule/sync"`,
		`"GET /api/v1/admin/version-rule"`, `"POST /api/v1/admin/version-rule"`} {
		if !strings.Contains(string(routes), want) {
			t.Fatalf("main.go missing %s", want)
		}
	}
	views, _ := adminWebFS.ReadFile("adminweb/views.js")
	if !strings.Contains(string(views), "Also stop very old apps") {
		t.Fatal("web page App version missing")
	}
}
