package httpapi

import (
	"regexp"
	"strings"
	"testing"
)

// Bundle 276: the web never shows a phone's default name as a person.
func TestAdminWeb276NoDefaultPhoneNames(t *testing.T) {
	views, comps := readAdminWeb27x(t, "views.js"), readAdminWeb27x(t, "components.js")
	for _, want := range []string{"function isDefaultPhoneName276", "function name276", "function phone276",
		"'my android phone'", "'LinkUp user'", "isDefaultPhoneName276, name276, phone276"} {
		if !strings.Contains(comps, want) {
			t.Fatalf("components.js misses %q", want)
		}
	}
	// No person name falls back to the phone name any more.
	bad := regexp.MustCompile(`(display_name|username)\s*\|\|\s*[a-z]+\.device_name`)
	if m := bad.FindString(views); m != "" {
		t.Fatalf("views.js still falls back to the phone name: %q", m)
	}
	for _, bad := range []string{"m.sender_name || 'Phone'", "u.display_name || u.username || ''"} {
		if strings.Contains(views, bad) {
			t.Fatalf("views.js still has %q", bad)
		}
	}
	if strings.Count(views, "ui.name276(") < 10 {
		t.Fatalf("names must go through ui.name276 (found %d)", strings.Count(views, "ui.name276("))
	}
	if !strings.Contains(views, "ui.phone276(n.device_name") {
		t.Fatal("map and network phone labels must use ui.phone276")
	}
}
