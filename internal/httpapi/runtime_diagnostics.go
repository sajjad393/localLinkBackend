package httpapi

import "net/http"

// SetRuntimeDiagnosticsProvider installs a read-only aggregate runtime snapshot.
// The provider must not return credentials, payloads, message bodies or device IDs.
func (a *API) SetRuntimeDiagnosticsProvider(provider func() map[string]any) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.runtimeProvider = provider
}

func (a *API) RuntimeDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	a.runtimeMu.RLock()
	provider := a.runtimeProvider
	a.runtimeMu.RUnlock()
	if provider == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": "runtime diagnostics unavailable",
		})
		return
	}
	snapshot := provider()
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"diagnostics": snapshot,
	})
}
