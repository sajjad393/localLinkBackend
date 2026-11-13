package httpapi

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
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
	if strings.HasPrefix(r.URL.Path, "/admin/fonts/") {
		a.AdminFont(w, r) // Bundle 216: Roboto for the web panel.
		return
	}
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
	// Bundle 299 S: the shell stays no-store; the big scripts/styles are
	// checked with an ETag (304 when unchanged) and sent gzipped, so the
	// panel opens fast on Wi-Fi. A new server build changes the ETag.
	var packed *packedAsset299
	if asset.file != "adminweb/index.html" {
		packed = packAsset299(asset.file, data)
	}
	if packed == nil {
		header.Set("Cache-Control", "no-store")
	} else {
		header.Set("Cache-Control", "no-cache")
		header.Set("Vary", "Accept-Encoding")
	}
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
		// blob: because conversation media is fetched with the bearer token and
		// rendered from object URLs (no cookie auth means <img>/<audio> cannot
		// point at the API directly).
		"img-src 'self' data: blob:",
		"media-src 'self' blob:",
		"connect-src 'self'",
		"font-src 'self'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; "))
	if packed != nil {
		etag := packed.etag
		body := data
		if packed.gz != nil && acceptsGzip299(r) {
			etag = packed.etagGz
			body = packed.gz
			header.Set("Content-Encoding", "gzip")
		}
		header.Set("ETag", etag)
		if etagMatch299(r.Header.Get("If-None-Match"), etag) {
			header.Del("Content-Encoding")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type packedAsset299 struct {
	etag, etagGz string
	gz           []byte // nil when gzip does not help
}

var packedAssets299 sync.Map // file -> *packedAsset299

func packAsset299(file string, data []byte) *packedAsset299 {
	if v, ok := packedAssets299.Load(file); ok {
		return v.(*packedAsset299)
	}
	sum := sha256.Sum256(data)
	tag := hex.EncodeToString(sum[:8])
	p := &packedAsset299{etag: `"` + tag + `"`, etagGz: `"` + tag + `-gz"`}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(data)
	if zw.Close() == nil && buf.Len() < len(data) {
		p.gz = buf.Bytes()
	}
	v, _ := packedAssets299.LoadOrStore(file, p)
	return v.(*packedAsset299)
}

func acceptsGzip299(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		part = strings.TrimSpace(part)
		name, params, _ := strings.Cut(part, ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			q := strings.TrimSpace(params)
			if v, ok := strings.CutPrefix(q, "q="); ok {
				f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
				return err != nil || f > 0
			}
			return true
		}
	}
	return false
}

func etagMatch299(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == etag || t == "*" {
			return true
		}
	}
	return false
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
