package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// adminWebFS contains the browser-based administrator console. The console is
// deliberately served by the same LocalLink backend so it uses the existing
// admin authentication/RBAC APIs and does not introduce another server.
//
//go:embed adminweb/index.html adminweb/styles.css adminweb/components.js adminweb/charts.js adminweb/views.js adminweb/app.js
var adminWebFS embed.FS

// adminWebAssets maps a request path to the embedded file and its content type.
var adminWebAssets = map[string]struct {
	file        string
	contentType string
}{
	"index.html":    {"adminweb/index.html", "text/html; charset=utf-8"},
	"styles.css":    {"adminweb/styles.css", "text/css; charset=utf-8"},
	"components.js": {"adminweb/components.js", "text/javascript; charset=utf-8"},
	"charts.js":     {"adminweb/charts.js", "text/javascript; charset=utf-8"},
	"views.js":      {"adminweb/views.js", "text/javascript; charset=utf-8"},
	"app.js":        {"adminweb/app.js", "text/javascript; charset=utf-8"},
}

// AdminWeb serves the administration console and its assets. Unknown paths
// without a file extension fall back to the console shell so refresh and deep
// links keep working; unknown assets return 404 rather than HTML.
func (a *API) AdminWeb(w http.ResponseWriter, r *http.Request) {
	name := assetNameFor(r.URL.Path)
	asset, ok := adminWebAssets[name]
	if !ok {
		// Extensionless paths (including nested ones) resolve to the console
		// shell; anything that looks like a file must exist or 404.
		if path.Ext(r.URL.Path) != "" {
			http.NotFound(w, r)
			return
		}
		asset = adminWebAssets["index.html"]
	}

	data, err := fs.ReadFile(adminWebFS, asset.file)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	header := w.Header()
	header.Set("Content-Type", asset.contentType)
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Frame-Options", "DENY")
	// The console is self-contained: no CDNs, no external fonts and no inline
	// scripts. Inline style attributes are used for layout values that come
	// from live data (meter widths), which is why styles allow them.
	header.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"connect-src 'self'",
		"font-src 'self'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; "))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// assetNameFor resolves "/admin/app.js" (and "/admin/" or "/admin") to the
// embedded asset name.
func assetNameFor(urlPath string) string {
	trimmed := strings.TrimPrefix(urlPath, "/admin")
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return "index.html"
	}
	return trimmed
}
