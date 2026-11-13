package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Bundle 213: the version gate in front of every app route.
//
//   - Apps from 3.0.54 send "X-LinkUp-Build: <n>" on every request and on
//     the socket. The server remembers it per phone (web page "App version").
//   - No rule, or min build 0: nothing changes.
//   - Build below the minimum after the stop time ("Stop now" / "Stop after
//     X days"): 426 update_required. The app turns read-only and shows Update.
//   - No header (apps before 3.0.54): 426 only when the admin turned on
//     "Also stop very old apps". Off by default.
//   - Never gated: the web admin panel and its API, health, the trust key,
//     the version rule itself, status / pairing info, group join pages.
const versionBuildHeader = "X-LinkUp-Build"

func versionGateExempt(path string) bool {
	if path == "/ws" {
		return false
	}
	if !strings.HasPrefix(path, "/api/v1/") {
		return true // /health, /ready, /admin, /join/...
	}
	for _, p := range []string{
		"/api/v1/admin/", "/api/v1/trust", "/api/v1/version-rule",
		"/api/v1/status", "/api/v1/pairing/info",
		"/api/v1/app/latest", // Bundle 299: an old phone must still find the new APK
	} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// VersionGate wraps the app routes (cmd/server/main.go).
func (a *API) VersionGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(r.Header.Get(versionBuildHeader))
		build, err := strconv.ParseInt(raw, 10, 64)
		has := raw != "" && err == nil && build > 0
		if has {
			a.noteDeviceBuild(strings.TrimSpace(r.Header.Get("X-Device-ID")), build)
		}
		if versionGateExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		rule, _ := a.currentVersionRule()
		if rule == nil || rule.MinBuild <= 0 {
			next.ServeHTTP(w, r)
			return
		}
		refuse := false
		reason := ""
		if !has {
			refuse, reason = rule.VeryOld, "very_old"
		} else if rule.stopped(build, time.Now().UTC()) {
			refuse, reason = true, "below_min"
		}
		if !refuse {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUpgradeRequired, map[string]any{
			"error":       "update_required",
			"reason":      reason,
			"min_version": rule.MinVersion,
			"min_build":   rule.MinBuild,
		})
	})
}
