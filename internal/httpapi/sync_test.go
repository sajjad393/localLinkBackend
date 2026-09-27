package httpapi

import (
	"encoding/base64"
	"strconv"
	"testing"
)

func TestMakeCursorRoundTrip(t *testing.T) {
	got := makeCursor(42)
	raw, err := base64.RawURLEncoding.DecodeString(got)
	if err != nil {
		t.Fatal(err)
	}
	v, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || v != 42 {
		t.Fatalf("unexpected cursor value: %q", raw)
	}
}

func TestMakeCursorMonotonicValues(t *testing.T) {
	if makeCursor(1) == makeCursor(2) {
		t.Fatal("different server sequences must produce different cursors")
	}
}
