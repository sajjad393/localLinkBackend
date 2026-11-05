package httpapi

import (
	"bytes"
	"compress/gzip"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Bundle 221 (b, d): Get chat from the phones, answer parts, delete completely.

func call221(t *testing.T, h http.HandlerFunc, method, path, id string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if id != "" {
		req.SetPathValue("id", id)
	}
	res := httptest.NewRecorder()
	h(res, req)
	return res
}

func owner221(s *blocks212) map[string]string {
	return map[string]string{"Authorization": "Bearer " + s.admin.Token}
}

// part221 builds one answer part the way the phone does: JSON -> gzip ->
// box for the reply key -> signed by the phone.
func part221(t *testing.T, p phone220, replyKey, answerID, actionID string, n, of int, plain map[string]any) signedBlob {
	t.Helper()
	raw, _ := json.Marshal(plain)
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	box, err := lockBox(replyKey, z.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(box))
	body, _ := json.Marshal(answerPartBody{V: 1, Kind: "admin_answer_part", AnswerID: answerID, ActionID: actionID,
		Device: p.auth.Device.ID, Part: n, Of: of, Sha256: hex.EncodeToString(sum[:]), Data: box})
	return signedBlob{Body: string(body), Sig: b64url(ed25519.Sign(p.sign, body))}
}

type answers221 struct {
	Acks     []signedBlob   `json:"acks"`
	Rejected map[string]int `json:"rejected"`
}

func upload221(t *testing.T, s *blocks212, from AuthResponse, parts ...signedBlob) answers221 {
	t.Helper()
	res := call221(t, s.api.AdminActionAnswers, http.MethodPost, "/api/v1/admin-actions/answers", "", map[string]any{"parts": parts}, directoryHeaders(from))
	if res.Code != http.StatusOK {
		t.Fatalf("answers %d %s", res.Code, res.Body.String())
	}
	var out answers221
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return out
}

func ack221(t *testing.T, s *blocks212, b signedBlob) answerAckBody {
	t.Helper()
	if !s.api.serverVerify(b) {
		t.Fatal("ack must be server-signed")
	}
	var a answerAckBody
	_ = json.Unmarshal([]byte(b.Body), &a)
	return a
}

func newChatRequest221(t *testing.T, s *blocks212, in map[string]any) (string, adminActionBody) {
	t.Helper()
	res := call221(t, s.api.AdminChatRequestCreate, http.MethodPost, "/api/v1/admin/chat-requests", "", in, owner221(s))
	if res.Code != http.StatusCreated {
		t.Fatalf("create %d %s", res.Code, res.Body.String())
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	var b signedBlob
	if err := s.api.store.DB().QueryRow(`SELECT body,sig FROM admin_actions WHERE id=?`, out.ID).Scan(&b.Body, &b.Sig); err != nil {
		t.Fatal(err)
	}
	c, why := s.api.checkAdminAction(b)
	if why != "" {
		t.Fatalf("card must verify: %s", why)
	}
	return out.ID, c
}

func TestChatRequestsOwnerOnly221(t *testing.T) {
	s := setup212(t)
	for _, role := range []string{"operator", "security", "auditor"} {
		token := adminAccountWithRole(t, s.api, role)
		h := map[string]string{"Authorization": "Bearer " + token}
		for name, fn := range map[string]http.HandlerFunc{
			"create": s.api.AdminChatRequestCreate, "list": s.api.AdminChatRequestList,
			"answer": s.api.AdminChatRequestAnswer, "purge": s.api.AdminPurge, "close": s.api.AdminChatRequestClose,
		} {
			res := call221(t, fn, http.MethodPost, "/x", "id-1", map[string]any{"kind": "direct"}, h)
			if res.Code != http.StatusForbidden {
				t.Fatalf("%s %s: %d", role, name, res.Code)
			}
		}
	}
}

func TestGetChatCardGoesToBothPeoplesPhones221(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	bilal := addPhoneKeys220(t, s.api, s.bilal)
	id, card := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Device.ID, "range": "7"})
	if card.Action != "get_chat" || card.Need != 1 || len(card.To) != 2 {
		t.Fatalf("card %s need=%d boxes=%d", card.Action, card.Need, len(card.To))
	}
	_, pub, _ := s.api.answerBoxKey()
	if card.ReplyKey != pub {
		t.Fatal("web requests are answered to the server key")
	}
	for _, p := range []phone220{ali, bilal} {
		plain := openMine220(t, p, card)
		chat := plain.Detail["chat"].(map[string]any)
		if chat["kind"] != "direct" || len(chat["devices"].([]any)) != 2 {
			t.Fatalf("chat %v", chat)
		}
		if plain.Detail["from"] != time.Now().AddDate(0, 0, -7).Format("2006-01-02") || plain.Detail["plus_day"] != true {
			t.Fatalf("range %v", plain.Detail)
		}
	}
	// Phones get it in the 220 sync like every card.
	out := sync220(t, s.api, s.ali, map[string]any{})
	found := false
	for _, c := range out.Cards {
		found = found || strings.Contains(c.Body, id)
	}
	if !found {
		t.Fatal("get_chat card must travel like other cards")
	}
	// Bad ranges and pickers.
	for _, in := range []map[string]any{
		{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.ali.Account.ID},
		{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID, "range": "dates", "from": "2026-11-09", "to": "2026-11-01"},
		{"kind": "group", "group_id": "nope"},
	} {
		if res := call221(t, s.api.AdminChatRequestCreate, http.MethodPost, "/x", "", in, owner221(s)); res.Code != http.StatusBadRequest {
			t.Fatalf("%v -> %d", in, res.Code)
		}
	}
}

func TestAnswerPartsHalfReadableResumableAndClose221(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	sync220(t, s.api, s.ali, map[string]any{"signing_key": ali.signingKey()})
	id, card := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	ans := id + "." + s.ali.Device.ID
	msg := func(mid, at string) map[string]any {
		return map[string]any{"id": mid, "created_at": at, "body": "text " + mid}
	}
	p1 := part221(t, ali, card.ReplyKey, ans, id, 1, 3, map[string]any{"chat": map[string]any{"kind": "direct"}, "messages": []any{msg("m2", "2026-11-02T10:00:00Z"), msg("m1", "2026-11-01T10:00:00Z")}})
	p2 := part221(t, ali, card.ReplyKey, ans, id, 2, 3, map[string]any{"messages": []any{msg("m3", "2026-11-03T10:00:00Z")}})
	p3 := part221(t, ali, card.ReplyKey, ans, id, 3, 3, map[string]any{"messages": []any{msg("m4", "2026-11-04T10:00:00Z")}})

	out := upload221(t, s, s.ali, p1, p2)
	if len(out.Acks) != 1 {
		t.Fatalf("acks %d", len(out.Acks))
	}
	if a := ack221(t, s, out.Acks[0]); a.Have != "1-2" || a.Of != 3 || a.All {
		t.Fatalf("ack %+v", a)
	}
	// Half an answer can be read.
	res := call221(t, s.api.AdminChatRequestAnswer, http.MethodGet, "/x", id, nil, owner221(s))
	var half struct {
		Messages []map[string]any `json:"messages"`
		Have     string           `json:"have"`
		Of       int              `json:"of"`
		Complete bool             `json:"complete"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &half)
	if res.Code != 200 || len(half.Messages) != 3 || half.Complete || half.Have != "1-2" || half.Messages[0]["id"] != "m1" {
		t.Fatalf("half %d %s", res.Code, res.Body.String())
	}
	// Same part again: kept once. A changed part and a fake signature are refused.
	bad := p3
	bad.Body = strings.Replace(bad.Body, `"part":3`, `"part":2`, 1)
	out = upload221(t, s, s.bilal, p1, bad)
	if out.Rejected["bad_sig"] != 1 {
		t.Fatalf("rejected %v", out.Rejected)
	}
	// A carrier (Bilal) brings the last part signed by Ali: complete.
	out = upload221(t, s, s.bilal, p3)
	if a := ack221(t, s, out.Acks[0]); !a.All || a.Have != "1-3" {
		t.Fatalf("final ack %+v", a)
	}
	res = call221(t, s.api.AdminChatRequestAnswer, http.MethodGet, "/x", id, nil, owner221(s))
	_ = json.Unmarshal(res.Body.Bytes(), &half)
	if !half.Complete || len(half.Messages) != 4 {
		t.Fatalf("full %s", res.Body.String())
	}
	var parts int
	_ = s.api.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_action_answers WHERE action_id=?`, id).Scan(&parts)
	if parts != 3 {
		t.Fatalf("parts kept %d", parts)
	}
	// List shows 3 of 3 and which chat it is.
	lres := call221(t, s.api.AdminChatRequestList, http.MethodGet, "/x", "", nil, owner221(s))
	if !strings.Contains(lres.Body.String(), `"parts":3,"of":3`) || !strings.Contains(lres.Body.String(), `"devices":[`) {
		t.Fatalf("list %s", lres.Body.String())
	}
	// Close: the server copy goes now.
	res = call221(t, s.api.AdminChatRequestClose, http.MethodPost, "/x", id, nil, owner221(s))
	_ = s.api.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_action_answers WHERE action_id=?`, id).Scan(&parts)
	if res.Code != 200 || parts != 0 {
		t.Fatalf("close %d parts=%d", res.Code, parts)
	}
}

func TestAnswerCopyGoes24hAfterFirstOpen221(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	id, card := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	upload221(t, s, s.ali, part221(t, ali, card.ReplyKey, id+".a", id, 1, 1, map[string]any{"messages": []any{}}))
	if n := s.api.CleanupAnswers221(time.Now().Add(48 * time.Hour)); n != 0 {
		t.Fatal("not opened yet: kept")
	}
	call221(t, s.api.AdminChatRequestAnswer, http.MethodGet, "/x", id, nil, owner221(s))
	if n := s.api.CleanupAnswers221(time.Now().Add(23 * time.Hour)); n != 0 {
		t.Fatal("kept for 24 h after the first open")
	}
	if n := s.api.CleanupAnswers221(time.Now().Add(25 * time.Hour)); n != 1 {
		t.Fatalf("removed %d after 24 h", n)
	}
}

func TestAdminPhoneAnswerFetchedOnlyByThatPhone221(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	card := s.actionCard220(t, "get_chat", 1, nil, "")
	out := sync220(t, s.api, s.adminDev, map[string]any{"cards": []signedBlob{card}})
	if out.Applied != 1 {
		t.Fatalf("offline get_chat card must reach the server (applied %d)", out.Applied)
	}
	var c adminActionBody
	_ = json.Unmarshal([]byte(card.Body), &c)
	// The answer is locked for the admin phone; use any key for the test.
	phoneBox := addPhoneKeys220(t, s.api, s.adminDev)
	key := b64url(mustPub221(t, phoneBox.box))
	upload221(t, s, s.ali, part221(t, ali, key, c.ID+".ali", c.ID, 1, 2, map[string]any{"messages": []any{}}))
	// The web can't read it.
	if res := call221(t, s.api.AdminChatRequestAnswer, http.MethodGet, "/x", c.ID, nil, owner221(s)); res.Code != http.StatusConflict {
		t.Fatalf("web read of an admin-phone answer: %d", res.Code)
	}
	fetch := func(from AuthResponse, have map[string]string) (int, bool) {
		res := call221(t, s.api.AdminActionAnswersFetch, http.MethodPost, "/x", "", map[string]any{"have": have}, directoryHeaders(from))
		var o struct {
			Parts []signedBlob `json:"parts"`
			More  bool         `json:"more"`
		}
		_ = json.Unmarshal(res.Body.Bytes(), &o)
		return len(o.Parts), o.More
	}
	if n, _ := fetch(s.ali, nil); n != 0 {
		t.Fatal("another phone must not fetch the admin's answer")
	}
	if n, _ := fetch(s.adminDev, nil); n != 1 {
		t.Fatalf("admin phone fetch = %d", n)
	}
	if n, _ := fetch(s.adminDev, map[string]string{c.ID + ".ali": "1"}); n != 0 {
		t.Fatal("parts the phone has are not sent again")
	}
	upload221(t, s, s.ali, part221(t, ali, key, c.ID+".ali", c.ID, 2, 2, map[string]any{"messages": []any{}}))
	fetch(s.adminDev, map[string]string{c.ID + ".ali": "1-2"})
	var left int
	_ = s.api.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_action_answers WHERE action_id=?`, c.ID).Scan(&left)
	if left != 0 {
		t.Fatal("the admin phone has all parts: the server copy goes")
	}
}

func TestPurgeDeletesServerCopiesAndSendsCard221(t *testing.T) {
	s := setup212(t)
	addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	addPhoneKeys220(t, s.api, s.adminDev)
	a, b, o := s.ali.Device.ID, s.bilal.Device.ID, s.adminDev.Device.ID
	ins := func(id, from, to string, days int) {
		at := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
		if _, err := s.api.store.DB().Exec(`INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status,server_seq) VALUES(?,?,?,'x',?,'sent',(SELECT COALESCE(MAX(server_seq),0)+1 FROM messages))`, id, from, to, at); err != nil {
			t.Fatal(err)
		}
	}
	ins("ab1", a, b, 1)
	ins("ab2", b, a, 2)
	ins("ab3", a, b, 20)
	ins("ao1", a, o, 1) // another chat of Ali: stays
	res := call221(t, s.api.AdminPurge, http.MethodPost, "/x", "", map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID, "scope": "range", "range": "7"}, owner221(s))
	if res.Code != http.StatusCreated {
		t.Fatalf("purge %d %s", res.Code, res.Body.String())
	}
	var out struct {
		ID      string `json:"id"`
		Removed int    `json:"removed_on_server"`
		Need    int    `json:"need"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if out.Removed != 2 || out.Need != 2 {
		t.Fatalf("purge out %+v", out)
	}
	for id, want := range map[string]int{"ab1": 0, "ab2": 0, "ab3": 1, "ao1": 1} {
		var n int
		_ = s.api.store.DB().QueryRow(`SELECT COUNT(*) FROM messages WHERE id=?`, id).Scan(&n)
		if n != want {
			t.Fatalf("%s left=%d want %d", id, n, want)
		}
	}
	x, y := a, b
	if x > y {
		x, y = y, x
	}
	var count int
	_ = s.api.store.DB().QueryRow(`SELECT count FROM conv_index WHERE kind='direct' AND a=? AND b=?`, x, y).Scan(&count)
	if count != 1 {
		t.Fatalf("conv_index count = %d, want 1", count)
	}
	// "1 of 2", then "2 of 2" from the phones' receipts.
	state, need, _ := actionState220(s.api, out.ID)
	if state != "pending" || need != 2 {
		t.Fatalf("card %s need %d", state, need)
	}
	var audit int
	_ = s.api.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action='admin_purge'`).Scan(&audit)
	if audit != 1 {
		t.Fatalf("audit rows %d", audit)
	}
	// Bad scopes.
	for _, in := range []map[string]any{
		{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID, "scope": "all"},
		{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID, "scope": "message"},
	} {
		if r := call221(t, s.api.AdminPurge, http.MethodPost, "/x", "", in, owner221(s)); r.Code != http.StatusBadRequest {
			t.Fatalf("%v -> %d", in, r.Code)
		}
	}
}

func TestGetFileCardFromAChatRequest221(t *testing.T) {
	s := setup212(t)
	addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	id, _ := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	res := call221(t, s.api.AdminChatRequestFile, http.MethodPost, "/x", id, map[string]any{"file_id": "f1", "message_id": "m1", "name": "IMG_1.jpg"}, owner221(s))
	if res.Code != http.StatusCreated {
		t.Fatalf("file %d %s", res.Code, res.Body.String())
	}
	_, card, _ := lastAction220(t, s.api, "get_file")
	if card.Parent != id || card.Need != 1 || len(card.To) != 2 {
		t.Fatalf("get_file card parent=%s need=%d boxes=%d", card.Parent, card.Need, len(card.To))
	}
	if res := call221(t, s.api.AdminChatRequestFile, http.MethodPost, "/x", "nope", map[string]any{"file_id": "f1"}, owner221(s)); res.Code != http.StatusNotFound {
		t.Fatalf("unknown request %d", res.Code)
	}
}

func TestPartRanges221(t *testing.T) {
	if got := partRanges([]int{5, 1, 2, 3, 7, 8}); got != "1-3,5,7-8" {
		t.Fatal(got)
	}
	m := parseRanges("1-3,5, 7-8")
	if len(m) != 6 || !m[5] || m[4] {
		t.Fatal(strconv.Itoa(len(m)))
	}
}

func mustPub221(t *testing.T, priv []byte) []byte {
	t.Helper()
	k, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return k.PublicKey().Bytes()
}
