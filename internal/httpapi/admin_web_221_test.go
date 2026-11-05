package httpapi

import (
	"strings"
	"testing"
)

// Bundle 221: Conversations asks the phones (list, request sheet, viewer,
// delete completely), the old server view stays reachable, and the privacy
// text says what the server keeps.
func TestAdminWeb221ConversationsFromPhones(t *testing.T) {
	read := func(name string) string {
		data, err := adminWebFS.ReadFile("adminweb/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	views, app, css := read("views.js"), read("app.js"), read("styles.css")
	for _, want := range []string{
		"/api/v1/admin/chat-requests", "/api/v1/admin/purge", "/answer?download=1", "/close", "/file",
		"Request from phones", "New request", "View request", "Send request",
		"Full chat", "Last 7 days", "Last 30 days", "Choose dates",
		"Outside the dates (phone clock)", "Deleted by user", "Get full ", "File removed after 60 days",
		"of ${v.of} parts \\u00b7 the rest is on the way", "this copy is deleted 24 h after you read it",
		"Delete completely", "Copy text", "count unknown", "also the copies users deleted. Can't be undone.",
		"Reading is recorded", "data-conversation-open", "data-conversation-back", "Access recorded",
		"The server keeps a message only until it is delivered (+7 days).",
		"get_chat: 'Get chat'", "delete_completely: 'Delete completely'",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
	if strings.Contains(views, "Message bodies are stored as users write them") {
		t.Fatal("the privacy page must not say message bodies stay on the server")
	}
	if !strings.Contains(app, "mountView('conversations', { quiet: true, refresh: true })") {
		t.Fatal("app.js: Conversations must refresh progress quietly")
	}
	for _, want := range []string{".info221", ".viewer221", ".del221p", ".sheet221", ".prog221", ".del221go"} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css misses %q", want)
		}
	}
}
