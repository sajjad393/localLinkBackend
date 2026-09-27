package httpapi

import "testing"

func TestHasControlChars(t *testing.T) {
	if hasControlChars("normal name") {
		t.Fatal("normal text marked as control characters")
	}
	if !hasControlChars("bad\nname") {
		t.Fatal("newline must be rejected")
	}
}

func TestSecureTokenEqual(t *testing.T) {
	if secureTokenEqual("abc", "abC") {
		t.Fatal("different tokens must not match")
	}
	if !secureTokenEqual("abc", "abc") {
		t.Fatal("equal tokens must match")
	}
}

func TestMessageRateLimit(t *testing.T) {
	_, a := newTestAPI(t)
	for i := 0; i < messagesPerMinute; i++ {
		if !a.allowMessage("device-a") {
			t.Fatalf("message %d unexpectedly rate limited", i+1)
		}
	}
	if a.allowMessage("device-a") {
		t.Fatal("expected the 121st message to be rate limited")
	}
	if !a.allowMessage("device-b") {
		t.Fatal("rate limit should be per device")
	}
}
