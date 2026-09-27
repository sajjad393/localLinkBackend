package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"locallink/backend/internal/store"
)

func TestReconcileCallIsAuthenticatedAndIdempotent(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	api := New(st)
	register := func(id string) string {
		res := doJSON(t, api.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": id, "name": id, "platform": "android"}, nil)
		if res.Code != http.StatusCreated {
			t.Fatalf("register %s: %d %s", id, res.Code, res.Body.String())
		}
		var p struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p.Token
	}
	tokenA := register("a")
	tokenB := register("b")
	call := map[string]any{"id": "call-direct-1", "caller_id": "a", "callee_id": "b", "status": "ended", "started_at": "2026-09-20T10:00:00Z", "answered_at": "2026-09-20T10:00:02Z", "ended_at": "2026-09-20T10:01:02Z", "duration_seconds": 60, "end_reason": "hangup"}
	unauth := doJSON(t, api.ReconcileCall, http.MethodPost, "/api/v1/calls/reconcile", call, nil)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", unauth.Code)
	}
	headersA := map[string]string{"Authorization": "Bearer " + tokenA, "X-Device-ID": "a"}
	headersB := map[string]string{"Authorization": "Bearer " + tokenB, "X-Device-ID": "b"}
	first := doJSON(t, api.ReconcileCall, http.MethodPost, "/api/v1/calls/reconcile", call, headersA)
	if first.Code != http.StatusCreated {
		t.Fatalf("first reconcile: %d %s", first.Code, first.Body.String())
	}
	second := doJSON(t, api.ReconcileCall, http.MethodPost, "/api/v1/calls/reconcile", call, headersB)
	if second.Code != http.StatusOK {
		t.Fatalf("idempotent reconcile: %d %s", second.Code, second.Body.String())
	}
	other := map[string]any{"id": "call-direct-1", "caller_id": "a", "callee_id": "b", "status": "connected", "started_at": "2026-09-20T10:00:00Z"}
	third := doJSON(t, api.ReconcileCall, http.MethodPost, "/api/v1/calls/reconcile", other, headersA)
	if third.Code != http.StatusOK {
		t.Fatalf("merge should preserve terminal state and succeed: %d %s", third.Code, third.Body.String())
	}
	var merged CallRecord
	if err := json.Unmarshal(third.Body.Bytes(), &merged); err != nil {
		t.Fatal(err)
	}
	if merged.Status != "ended" || merged.DurationSeconds != 60 {
		t.Fatalf("terminal call was downgraded: %+v", merged)
	}
}
