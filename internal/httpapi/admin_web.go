package httpapi

import (
	"embed"
	"net/http"
	"strings"
)

// adminWebFS contains the browser-based administrator console. The console is
// deliberately served by the same LocalLink backend so it uses the existing
// admin authentication/RBAC APIs and does not introduce another server.
//
//go:embed adminweb/index.html adminweb/app.js adminweb/styles.css
var adminWebFS embed.FS

func (a *API) AdminWeb(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin")
	if path == "" || path == "/" {
		path = "index.html"
	} else {
		path = strings.TrimPrefix(path, "/")
	}

	if path != "index.html" && path != "app.js" && path != "styles.css" {
		path = "index.html"
	}

	data, err := adminWebFS.ReadFile("adminweb/" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch path {
	case "index.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case "app.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case "styles.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
