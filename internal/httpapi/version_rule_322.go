package httpapi

// Bundle 322 (W4): the App version page picks the build from what is really
// on this server. V1: GET /api/v1/admin/version-rule also returns `uploads`
// (the APKs the owner put here). V5: a rule for a build that is not on the
// server is refused, but only when the page asks for it (`require_upload322`),
// so scripted / older callers keep working exactly as in 321.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// versionUpload322 is one row of the pick list. Additive JSON: an old web
// bundle ignores it.
type versionUpload322 struct {
	ID         int64  `json:"id"`
	Version    string `json:"version"`
	Build      int64  `json:"build"`
	FileName   string `json:"file_name"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	AddedAt    string `json:"added_at"`
	AddedBy    string `json:"added_by"`
	Downloads  int64  `json:"downloads"`
	Current    bool   `json:"current"`          // the download this server serves now
	IsRule     bool   `json:"is_rule"`          // the saved rule points at this build
	OnDisk     bool   `json:"on_disk"`          // the file is still there
	PhonesHere int    `json:"phones_here"`      // 30 days, phones on exactly this build
	Newest     bool   `json:"newest,omitempty"` // the newest upload (by build)
}

// versionUploads322 lists the APKs on this server, newest build first.
func (a *API) versionUploads322(ruleBuild int64) []versionUpload322 {
	out := []versionUpload322{}
	rows, err := a.store.DB().Query(`SELECT ` + apkCols299 + ` FROM app_apk_299 ORDER BY build DESC, id DESC LIMIT 50`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		row, scanErr := scanAPK299(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, versionUpload322{
			ID: row.ID, Version: row.Version, Build: row.Build, FileName: row.FileName,
			Size: row.Size, SHA256: row.SHA256, AddedAt: row.AddedAt, AddedBy: row.AddedBy,
			Downloads: row.Downloads, Current: row.Current,
			IsRule: ruleBuild > 0 && row.Build == ruleBuild,
			OnDisk: a.apkOnDisk299(row.FileName),
		})
	}
	if len(out) > 0 {
		out[0].Newest = true
	}
	// How many phones are on exactly this build (30 days). One query.
	since := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	phones := map[int64]int{}
	if pr, err := a.store.DB().Query(`SELECT build,COUNT(*) FROM device_builds WHERE seen_at>=? GROUP BY build`, since); err == nil {
		for pr.Next() {
			var b int64
			var n int
			if pr.Scan(&b, &n) == nil {
				phones[b] = n
			}
		}
		pr.Close()
	}
	for i := range out {
		out[i].PhonesHere = phones[out[i].Build]
	}
	return out
}

// buildOnServer322: is this build one of the APKs this server has?
func (a *API) buildOnServer322(build int64) bool {
	if build < 1 {
		return false
	}
	var n int
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM app_apk_299 WHERE build=?`, build).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// uploadPayload322: the small shape the page's pick list needs.
func (a *API) uploadPayload322(ruleBuild int64) map[string]any {
	uploads := a.versionUploads322(ruleBuild)
	var newest any
	if len(uploads) > 0 {
		newest = uploads[0]
	}
	return map[string]any{"uploads": uploads, "newest_upload": newest}
}

// versionRuleGet322 wraps the 321 GET body with the new keys.
func (a *API) versionRuleGet322(w http.ResponseWriter, r *http.Request, body map[string]any, ruleBuild int64) {
	for k, v := range a.uploadPayload322(ruleBuild) {
		body[k] = v
	}
	writeJSON(w, http.StatusOK, body)
}

// apkOnDisk299: is the file still in the APK folder? (nil-safe for tests)
func (a *API) apkOnDisk299(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(a.apkDir299(), name))
	return err == nil
}
