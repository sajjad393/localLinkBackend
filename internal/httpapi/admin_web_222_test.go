package httpapi

import (
	"strings"
	"testing"
)

// Bundle 222: web Users lists offline accounts phones saw nearby, the
// Recovery page shows the kinds, and Approve works and shows the code.
func TestAdminWeb222UsersAndRecovery(t *testing.T) {
	read := func(name string) string {
		data, err := adminWebFS.ReadFile("adminweb/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	views, app, css := read("views.js"), read("app.js"), read("styles.css")
	for _, want := range []string{
		"/api/v1/admin/users?limit=200&include=not_on_server", "Not on server yet", "Not on server ${nos.length}",
		"(may change if taken)", "waiting for a phone with internet", "No actions until the account joins",
		"'No code'", "'Reset with code'", "'Lost phone'", "Done by the user (audit only)", "waits until you answer",
		"Password help requests and lost phones",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
	for _, want := range []string{"?id=${encodeURIComponent(id || '')}", "showCode222(result.recovery_credential", "One-time code", "Give this code to the person yourself"} {
		if !strings.Contains(app, want) {
			t.Fatalf("app.js misses %q", want)
		}
	}
	if !strings.Contains(css, ".code222") {
		t.Fatal("styles.css misses .code222")
	}
}
