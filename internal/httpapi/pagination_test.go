package httpapi

import (
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestListCursorRoundTripAndScopeBinding(t *testing.T) {
	encoded := encodeListCursor("messages:a:b", "2026-09-23T00:00:00Z", "message-123", false)
	if len(encoded) == 0 {
		t.Fatal("cursor was empty")
	}
	req := httptest.NewRequest("GET", "/messages?cursor="+encoded+"&limit=50", nil)
	page, err := parsePageParams(req, "messages:a:b")
	if err != nil {
		t.Fatal(err)
	}
	if page.Limit != 50 || page.Cursor == nil || page.Cursor.Key != "2026-09-23T00:00:00Z" || page.Cursor.ID != "message-123" {
		t.Fatalf("unexpected page params: %+v", page)
	}
	badReq := httptest.NewRequest("GET", "/messages?cursor="+encoded, nil)
	if _, err := parsePageParams(badReq, "group-messages:g1"); err == nil {
		t.Fatal("cross-scope cursor was accepted")
	}
}

func TestListCursorRejectsMalformedAndOversizedValues(t *testing.T) {
	for _, raw := range []string{"not-base64", base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"messages:a:b","k":"","i":"x","d":false}`)), string(make([]byte, 2000))} {
		req := httptest.NewRequest("GET", "/messages?cursor="+url.QueryEscape(raw), nil)
		if _, err := parsePageParams(req, "messages:a:b"); err == nil {
			t.Fatalf("accepted malformed cursor %q", raw[:min(20, len(raw))])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
