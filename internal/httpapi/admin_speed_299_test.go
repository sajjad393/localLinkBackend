package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Bundle 299 S: the admin panel opens fast (gzip + ETag, fewer DB writes).

func adminAsset299(t *testing.T, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	_, api := newTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	api.AdminWeb(rec, req)
	return rec
}

func TestAdminAssetsGzipAndETag299(t *testing.T) {
	raw, _ := fs.ReadFile(adminWebFS, "adminweb/views.js")
	rec := adminAsset299(t, "/admin/views.js", map[string]string{"Accept-Encoding": "gzip, deflate"})
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("code %d enc %q cc %q", rec.Code, rec.Header().Get("Content-Encoding"), rec.Header().Get("Cache-Control"))
	}
	if rec.Body.Len()*2 > len(raw) {
		t.Fatalf("gzip too big: %d of %d", rec.Body.Len(), len(raw))
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if !bytes.Equal(got, raw) {
		t.Fatal("gzip body differs")
	}
	etag := rec.Header().Get("ETag")
	again := adminAsset299(t, "/admin/views.js", map[string]string{"Accept-Encoding": "gzip", "If-None-Match": etag})
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("304 expected, got %d (%d bytes)", again.Code, again.Body.Len())
	}
	plain := adminAsset299(t, "/admin/views.js", nil)
	if plain.Header().Get("Content-Encoding") != "" || !bytes.Equal(plain.Body.Bytes(), raw) || plain.Header().Get("ETag") == etag {
		t.Fatal("no gzip asked: plain body with its own ETag")
	}
	if adminAsset299(t, "/admin/views.js", map[string]string{"Accept-Encoding": "gzip;q=0"}).Header().Get("Content-Encoding") != "" {
		t.Fatal("q=0 means no gzip")
	}
	shell := adminAsset299(t, "/admin/", map[string]string{"Accept-Encoding": "gzip"})
	if shell.Header().Get("Cache-Control") != "no-store" || shell.Header().Get("Content-Encoding") != "" {
		t.Fatal("the shell stays no-store and plain")
	}
}

func TestAdminLastSeenOncePerMinute299(t *testing.T) {
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	for s, want := range map[string]bool{
		now.Add(-10 * time.Second).Format(time.RFC3339Nano): false,
		now.Add(-61 * time.Second).Format(time.RFC3339Nano): true,
		now.Add(5 * time.Minute).Format(time.RFC3339Nano):   true,
		"": true,
	} {
		if got := adminLastSeenStale299(s, now); got != want {
			t.Fatalf("%q got %v", s, got)
		}
	}
}
