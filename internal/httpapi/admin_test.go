package httpapi

import "testing"

func TestAdminStaticTokenIsNeverAllowedInProduction(t *testing.T) {
	if adminStaticTokenAllowed("production") {
		t.Fatal("static admin token must not be enabled in production")
	}
	if adminStaticTokenAllowed("test") != true || adminStaticTokenAllowed("development") != true {
		t.Fatal("static admin token should remain available only to explicit test/development modes")
	}
}
