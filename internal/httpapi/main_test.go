package httpapi

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("LOCALLINK_ENV", "test")
	// Bundle 222: no forced disk flush in these tests (slow on Windows).
	_ = os.Setenv("LOCALLINK_TEST_FAST_DB", "1")
	// Keep account-heavy tests fast while production retains the 180,000-round
	// password KDF configured by auth.go.
	passwordIterations = 1000
	if os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME") == "" {
		_ = os.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME", "test-admin")
	}
	if os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD") == "" {
		_ = os.Setenv("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD", "test-admin-password-123")
	}
	os.Exit(m.Run())
}
