package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeDiagnosticsRequiresAdmin(t *testing.T) {
	_, a := newTestAPI(t)
	a.SetRuntimeDiagnosticsProvider(func() map[string]any {
		return map[string]any{"requests": 12, "active_requests": 1}
	})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime-diagnostics", nil)
	w := httptest.NewRecorder()
	a.RuntimeDiagnostics(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", w.Code)
	}
}
