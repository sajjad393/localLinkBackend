package httpapi

import (
	"net/http"
	"testing"
)

// Bundle 317 (G5): a refused Get file shows why ("file_gone") in the list.
func TestRefusedFileShowsWhy317(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	id, _ := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	res := call221(t, s.api.AdminChatRequestFile, http.MethodPost, "/x", id, map[string]any{"file_id": "f1", "message_id": "m1", "name": "VID_1.mp4"}, owner221(s))
	if res.Code != http.StatusCreated {
		t.Fatalf("file %d %s", res.Code, res.Body.String())
	}
	fileID, _, _ := lastAction220(t, s.api, "get_file")
	if r := row307(t, s, fileID); r.Refused != "" {
		t.Fatalf("not answered yet: refused=%q", r.Refused)
	}
	// Regression: the chat request itself has no refusal.
	if r := row307(t, s, id); r.Refused != "" {
		t.Fatalf("chat request refused=%q", r.Refused)
	}
	sync220(t, s.api, s.ali, map[string]any{"signing_key": ali.signingKey(), "receipts": []signedBlob{ali.done(fileID, "refused:file_gone")}})
	r := row307(t, s, fileID)
	if r.Refused != "file_gone" {
		t.Fatalf("refused file: want file_gone, got %q (state %s)", r.Refused, r.State)
	}
	if r.FileID != "f1" || r.Parent != id {
		t.Fatalf("file row: %+v", r)
	}
	// An "ok" receipt is not a refusal.
	res = call221(t, s.api.AdminChatRequestFile, http.MethodPost, "/x", id, map[string]any{"file_id": "f2", "message_id": "m2", "name": "IMG_2.jpg"}, owner221(s))
	if res.Code != http.StatusCreated {
		t.Fatalf("file2 %d %s", res.Code, res.Body.String())
	}
	f2, _, _ := lastAction220(t, s.api, "get_file")
	sync220(t, s.api, s.ali, map[string]any{"receipts": []signedBlob{ali.done(f2, "ok")}})
	if r := row307(t, s, f2); r.Refused != "" {
		t.Fatalf("ok receipt: refused=%q", r.Refused)
	}
}
