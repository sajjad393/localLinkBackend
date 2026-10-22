package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateProductionSecretsRequiresPairingCode(t *testing.T) {
	t.Setenv("LOCALLINK_ENV", "production")
	// The developer machine may run a non-TLS server (LOCALLINK_REQUIRE_TLS=false)
	// in its user environment; this test must not depend on ambient shell state.
	t.Setenv("LOCALLINK_REQUIRE_TLS", "true")
	t.Setenv("LOCALLINK_TLS_CERT", "test-cert.pem")
	t.Setenv("LOCALLINK_TLS_KEY", "test-key.pem")
	t.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME", "")
	t.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD", "")
	t.Setenv("LOCALLINK_PAIRING_CODE", "")
	if err := validateProductionSecrets(); err == nil || !strings.Contains(err.Error(), "LOCALLINK_PAIRING_CODE is required") {
		t.Fatalf("missing pairing code should fail closed, got %v", err)
	}

	t.Setenv("LOCALLINK_PAIRING_CODE", "short")
	if err := validateProductionSecrets(); err == nil || !strings.Contains(err.Error(), "at least 16") {
		t.Fatalf("short pairing code should fail, got %v", err)
	}

	t.Setenv("LOCALLINK_PAIRING_CODE", "production-pairing-2026")
	if err := validateProductionSecrets(); err != nil {
		t.Fatalf("valid production pairing configuration rejected: %v", err)
	}
}

func TestValidateDevelopmentAndTestMayOmitPairingCode(t *testing.T) {
	for _, env := range []string{"development", "test"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("LOCALLINK_ENV", env)
			t.Setenv("LOCALLINK_PAIRING_CODE", "")
			if err := validateProductionSecrets(); err != nil {
				t.Fatalf("%s should not require production pairing code: %v", env, err)
			}
		})
	}
}

func TestLimitRequestBody(t *testing.T) {
	large := strings.Repeat("x", maxGenericRequestBody+1)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(large))
	req.Header.Set("Content-Type", "application/json")
	called := false
	h := limitRequestBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		buf := make([]byte, maxGenericRequestBody+1)
		_, err := io.ReadFull(r.Body, buf)
		if err == nil {
			t.Fatal("oversized request body was not limited")
		}
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if !called || w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("middleware did not enforce generic body limit: called=%v status=%d", called, w.Code)
	}
}

func TestValidateProductionSecretsRejectsExampleAdminPassword(t *testing.T) {
	t.Setenv("LOCALLINK_ENV", "production")
	t.Setenv("LOCALLINK_TLS_CERT", "test-cert.pem")
	t.Setenv("LOCALLINK_TLS_KEY", "test-key.pem")
	t.Setenv("LOCALLINK_REQUIRE_TLS", "true")
	t.Setenv("LOCALLINK_PAIRING_CODE", "production-pairing-secret-2026")
	t.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME", "admin")
	t.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD", "change-this-to-a-strong-password")
	if err := validateProductionSecrets(); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("example admin password should fail closed, got %v", err)
	}
}

func TestValidateProductionSecretsRejectsWeakPairingAndCleartext(t *testing.T) {
	t.Setenv("LOCALLINK_ENV", "production")
	t.Setenv("LOCALLINK_TLS_CERT", "test-cert.pem")
	t.Setenv("LOCALLINK_TLS_KEY", "test-key.pem")
	t.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME", "")
	t.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD", "")
	t.Setenv("LOCALLINK_PAIRING_CODE", "too-short")
	t.Setenv("LOCALLINK_REQUIRE_TLS", "true")
	if err := validateProductionSecrets(); err == nil || !strings.Contains(err.Error(), "at least 16") {
		t.Fatalf("weak pairing code should fail closed, got %v", err)
	}
	t.Setenv("LOCALLINK_PAIRING_CODE", "production-pairing-secret-2026")
	t.Setenv("LOCALLINK_REQUIRE_TLS", "false")
	if err := validateProductionSecrets(); err == nil || !strings.Contains(err.Error(), "LOCALLINK_REQUIRE_TLS") {
		t.Fatalf("production cleartext mode should fail closed, got %v", err)
	}
}

func TestValidateProductionSecretsAdminCredentialsMustBePaired(t *testing.T) {
	t.Setenv("LOCALLINK_ENV", "production")
	t.Setenv("LOCALLINK_TLS_CERT", "test-cert.pem")
	t.Setenv("LOCALLINK_TLS_KEY", "test-key.pem")
	t.Setenv("LOCALLINK_REQUIRE_TLS", "true")
	t.Setenv("LOCALLINK_PAIRING_CODE", "production-pairing-secret-2026")
	t.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME", "admin")
	t.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD", "")
	if err := validateProductionSecrets(); err == nil || !strings.Contains(err.Error(), "configured together") {
		t.Fatalf("partial admin bootstrap credentials should fail closed, got %v", err)
	}
}

func TestRequestLoggingAddsRequestIDAndRecordsStatus(t *testing.T) {
	metrics := newRuntimeMetrics()
	h := requestLogging(metrics, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("X-Request-ID"); len(got) != 32 {
		t.Fatalf("request id should be 32 hex chars, got %q", got)
	}
	if w.Code != http.StatusAccepted || metrics.requests.Load() != 1 || metrics.successes.Load() != 1 {
		t.Fatalf("metrics/status mismatch code=%d requests=%d success=%d", w.Code, metrics.requests.Load(), metrics.successes.Load())
	}
}

func TestRequestRecoveryReturnsGeneric500(t *testing.T) {
	metrics := newRuntimeMetrics()
	h := requestLogging(metrics, requestRecovery(metrics, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("sensitive panic value should not be returned")
	})))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("panic should become generic 500, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "sensitive panic value") || !strings.Contains(w.Body.String(), "request_id") {
		t.Fatalf("panic response leaked detail: %s", w.Body.String())
	}
	if metrics.panics.Load() != 1 {
		t.Fatalf("expected one recovered panic, got %d", metrics.panics.Load())
	}
}
