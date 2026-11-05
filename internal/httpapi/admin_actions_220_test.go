package httpapi

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Bundle 220: every admin action goes to all phones (signed cards, locked
// boxes, done receipts, stop notes, cancel, offline admin cards, list).

// Shared Go / Dart test vector (the app test
// test/bundle220_admin_actions_test.dart opens the same box).
const vector220Plain = `{"device":"dev-x","user":"u-x","detail":{"session_id":"s1"}}`
const vector220Box = "WGmv9FBUlzLLqu1eXfmzCm2jHLDldCutWtShp2jxpnugoaKjpKWmp6ipqqtJA6VSjdvGQwR3Gs6Kc70uq4RmHkGTg3hWfZPYtL1_VFjFTqSwpNASoP2TDpzOD3nSnn4tKV2URhHfpv9kxMm3XG-7p7mRT__lqyb6"

func seq220(start byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = start + byte(i)
	}
	return out
}

func TestBoxVectorAndRoundTrip220(t *testing.T) {
	priv := seq220(1, 32)
	k, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pub := b64url(k.PublicKey().Bytes())
	box, err := lockBoxFixed(seq220(33, 32), seq220(0xA0, 12), pub, []byte(vector220Plain))
	if err != nil {
		t.Fatal(err)
	}
	if box != vector220Box {
		t.Fatalf("vector changed:\n%s", box)
	}
	plain, err := openBox(priv, box)
	if err != nil || string(plain) != vector220Plain {
		t.Fatalf("open: %v %s", err, plain)
	}
	// Random boxes open too, and a wrong key can't open them.
	rb, err := lockBox(pub, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if p, err := openBox(priv, rb); err != nil || string(p) != "hello" {
		t.Fatalf("random box: %v %s", err, p)
	}
	if _, err := openBox(seq220(2, 32), rb); err == nil {
		t.Fatal("a wrong key must not open the box")
	}
}

type phone220 struct {
	auth AuthResponse
	box  []byte // X25519 private key
	sign ed25519.PrivateKey
}

func addPhoneKeys220(t *testing.T, a *API, auth AuthResponse) phone220 {
	t.Helper()
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.DB().Exec(`INSERT INTO device_identity_keys(device_id,key_version,public_key,created_at) VALUES(?,1,?,?)`,
		auth.Device.ID, b64url(k.PublicKey().Bytes()), nowStamp()); err != nil {
		t.Fatal(err)
	}
	_, sk, _ := ed25519.GenerateKey(rand.Reader)
	return phone220{auth: auth, box: k.Bytes(), sign: sk}
}

func (p phone220) signingKey() string { return b64url(p.sign.Public().(ed25519.PublicKey)) }

func (p phone220) done(actionID, result string) signedBlob {
	raw, _ := json.Marshal(adminActionDoneBody{V: 1, Kind: "admin_action_done", ActionID: actionID, Device: p.auth.Device.ID,
		User: p.auth.Account.ID, Result: result, At: nowStamp(), Build: 131})
	return signedBlob{Body: string(raw), Sig: b64url(ed25519.Sign(p.sign, raw))}
}

type syncOut220 struct {
	Cards    []signedBlob `json:"cards"`
	States   []signedBlob `json:"states"`
	More     bool         `json:"more"`
	Applied  int          `json:"applied"`
	Rejected int          `json:"rejected"`
}

func sync220(t *testing.T, a *API, from AuthResponse, in map[string]any) syncOut220 {
	t.Helper()
	res := doJSON(t, a.AdminActionsSync, http.MethodPost, "/api/v1/admin-actions/sync", in, directoryHeaders(from))
	if res.Code != http.StatusOK {
		t.Fatalf("sync %d %s", res.Code, res.Body.String())
	}
	var out syncOut220
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return out
}

func setStatus220(t *testing.T, s *blocks212, userID, status string) {
	t.Helper()
	res := doJSON(t, s.api.AdminSetUserStatus, http.MethodPost, "/api/v1/admin/user/status?id="+userID+"&status="+status, nil,
		map[string]string{"Authorization": "Bearer " + s.admin.Token})
	if res.Code != http.StatusOK {
		t.Fatalf("status %s: %d %s", status, res.Code, res.Body.String())
	}
}

func lastAction220(t *testing.T, a *API, action string) (string, adminActionBody, signedBlob) {
	t.Helper()
	var id string
	var b signedBlob
	if err := a.store.DB().QueryRow(`SELECT id,body,sig FROM admin_actions WHERE action=? ORDER BY made_at DESC LIMIT 1`, action).Scan(&id, &b.Body, &b.Sig); err != nil {
		t.Fatalf("no %s card: %v", action, err)
	}
	c, reason := a.checkAdminAction(b)
	if reason != "" {
		t.Fatalf("card must verify: %s", reason)
	}
	return id, c, b
}

func actionState220(a *API, id string) (string, int, int) {
	var state string
	var need, done int
	_ = a.store.DB().QueryRow(`SELECT state,need,done FROM admin_actions WHERE id=?`, id).Scan(&state, &need, &done)
	return state, need, done
}

func openMine220(t *testing.T, p phone220, c adminActionBody) adminActionBoxPlain {
	t.Helper()
	for _, b := range c.To {
		if raw, err := openBox(p.box, b.Box); err == nil {
			var plain adminActionBoxPlain
			if err := json.Unmarshal(raw, &plain); err != nil {
				t.Fatal(err)
			}
			return plain
		}
	}
	t.Fatal("no box opens for this phone")
	return adminActionBoxPlain{}
}

func TestWebBlockGoesToAllPhonesAndDoneStops220(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	bilal := addPhoneKeys220(t, s.api, s.bilal)
	setStatus220(t, s, s.ali.Account.ID, "suspended")
	id, c, _ := lastAction220(t, s.api, "block")
	if c.Signer != "server" || c.Need != 1 || len(c.To) != 1 || c.Inner == nil {
		t.Fatalf("bad card %+v", c)
	}
	if strings.Contains(c.To[0].K, s.ali.Device.ID) {
		t.Fatal("the box id must not name the phone")
	}
	if inner, reason := s.api.checkAdminBlock(*c.Inner); reason != "" || inner.Action != "block" || inner.TargetUserID != s.ali.Account.ID {
		t.Fatalf("inner must be the 212 block card: %+v %s", inner, reason)
	}
	if got := openMine220(t, ali, c); got.Device != s.ali.Device.ID || got.User != s.ali.Account.ID {
		t.Fatalf("box: %+v", got)
	}
	for _, b := range c.To {
		if _, err := openBox(bilal.box, b.Box); err == nil {
			t.Fatal("another phone must not open the box")
		}
	}
	// A non-friend phone gets the card to carry.
	out := sync220(t, s.api, s.bilal, map[string]any{})
	if len(out.Cards) != 1 {
		t.Fatalf("bilal must get the card to carry, got %d", len(out.Cards))
	}
	// Ali's phone (blocked, can't pass device auth) gets it from /mine and
	// sends its signed Done.
	res := doJSON(t, s.api.AdminActionsMine, http.MethodPost, "/api/v1/admin-actions/mine",
		map[string]any{"signing_key": ali.signingKey(), "receipts": []signedBlob{ali.done(id, "ok")}}, directoryHeaders(s.ali))
	if res.Code != http.StatusOK {
		t.Fatalf("mine %d %s", res.Code, res.Body.String())
	}
	if state, need, done := actionState220(s.api, id); state != "done" || need != 1 || done != 1 {
		t.Fatalf("state %s %d/%d", state, done, need)
	}
	// Bilal has the card: he gets the server-signed stop note.
	out = sync220(t, s.api, s.bilal, map[string]any{"have": []string{id}})
	if len(out.Cards) != 0 || len(out.States) != 1 || !s.api.serverVerify(out.States[0]) {
		t.Fatalf("stop note expected: %+v", out)
	}
	var st adminActionStateBody
	_ = json.Unmarshal([]byte(out.States[0].Body), &st)
	if st.ID != id || st.State != "done" || st.Done != 1 {
		t.Fatalf("stop note %+v", st)
	}
}

func TestCarriedReceiptCountsOnceTheKeyIsKnown220(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	setStatus220(t, s, s.ali.Account.ID, "suspended")
	id, _, _ := lastAction220(t, s.api, "block")
	// Bilal carries Ali's Done before the server knows Ali's signing key.
	sync220(t, s.api, s.bilal, map[string]any{"receipts": []signedBlob{ali.done(id, "ok")}})
	if state, _, done := actionState220(s.api, id); state != "pending" || done != 0 {
		t.Fatalf("an unchecked receipt must not count: %s %d", state, done)
	}
	// A fake receipt (wrong key) is dropped when the key arrives.
	fake := phone220{auth: s.ali, sign: func() ed25519.PrivateKey { _, k, _ := ed25519.GenerateKey(rand.Reader); return k }()}
	sync220(t, s.api, s.bilal, map[string]any{"receipts": []signedBlob{fake.done(id, "ok")}})
	s.api.setPresence(s.ali.Device.ID, true, false, DevicePresence{SigningPublicKey: ali.signingKey()})
	if state, _, done := actionState220(s.api, id); state != "done" || done != 1 {
		t.Fatalf("after the key: %s %d", state, done)
	}
	var via string
	_ = s.api.store.DB().QueryRow(`SELECT via FROM admin_action_receipts WHERE action_id=?`, id).Scan(&via)
	if via != "carried" {
		t.Fatalf("via %q", via)
	}
}

func TestOffAndActiveUseBlockCards220(t *testing.T) {
	s := setup212(t)
	addPhoneKeys220(t, s.api, s.ali)
	aliID := s.ali.Account.ID
	setStatus220(t, s, aliID, "disabled")
	_, c, _ := lastAction220(t, s.api, "off")
	inner, reason := s.api.checkAdminBlock(*c.Inner)
	if reason != "" || inner.Action != "block" {
		t.Fatalf("off must carry a block card: %+v %s", inner, reason)
	}
	if s.status(t, aliID) != "disabled" {
		t.Fatal("off stays disabled")
	}
	setStatus220(t, s, aliID, "active")
	_, c, _ = lastAction220(t, s.api, "active")
	inner, _ = s.api.checkAdminBlock(*c.Inner)
	if inner.Action != "unblock" {
		t.Fatalf("active after off must carry an unblock card: %+v", inner)
	}
	var action string
	_ = s.api.store.DB().QueryRow(`SELECT action FROM admin_blocks WHERE target_user_id=?`, aliID).Scan(&action)
	if action != "unblock" || s.status(t, aliID) != "active" {
		t.Fatalf("server block %s, status %s", action, s.status(t, aliID))
	}
}

func (s *blocks212) actionCard220(t *testing.T, action string, need int, inner *signedBlob, cancels string) signedBlob {
	t.Helper()
	cert := s.cert
	body := adminActionBody{V: 1, Kind: "admin_action", ID: uuid.NewString(), Action: action, Seq: time.Now().UnixMilli(),
		At: nowStamp(), By: "test-admin", Signer: "admin", Cert: &cert, Need: need, To: []adminActionBox{{K: "abc", Box: "x"}},
		Inner: inner, Cancels: cancels}
	raw, _ := json.Marshal(body)
	return signedBlob{Body: string(raw), Sig: b64url(ed25519.Sign(s.phoneKey, raw))}
}

func TestOfflineAdminCardLabelAndCompanion220(t *testing.T) {
	s := setup212(t)
	addPhoneKeys220(t, s.api, s.ali)
	second := registerDirectoryUser(t, s.api, "ali212b", "dev-ali-212-b")
	_, _ = s.api.store.DB().Exec(`UPDATE devices SET user_id=? WHERE id=?`, s.ali.Account.ID, second.Device.ID)
	addPhoneKeys220(t, s.api, second)
	block := s.record(t, s.ali.Account.ID, "block", 1, s.ali.Device.ID)
	card := s.actionCard220(t, "block", 1, &block, "")
	var c adminActionBody
	_ = json.Unmarshal([]byte(card.Body), &c)
	// A stranger carries it to the server: saved, no details yet.
	out := sync220(t, s.api, s.bilal, map[string]any{"cards": []signedBlob{card}})
	if out.Applied != 1 {
		t.Fatalf("applied %d rejected %d", out.Applied, out.Rejected)
	}
	label := map[string]any{"id": c.ID, "user_id": s.ali.Account.ID, "label": "@ali212", "devices": []string{s.ali.Device.ID}}
	// Labels from a phone that is not the admin's phone are ignored.
	sync220(t, s.api, s.bilal, map[string]any{"labels": []any{label}})
	var got string
	_ = s.api.store.DB().QueryRow(`SELECT target_label FROM admin_actions WHERE id=?`, c.ID).Scan(&got)
	if !strings.Contains(got, "made offline") {
		t.Fatalf("label from a stranger must be ignored: %q", got)
	}
	sync220(t, s.api, s.adminDev, map[string]any{"labels": []any{label}})
	_ = s.api.store.DB().QueryRow(`SELECT target_label FROM admin_actions WHERE id=?`, c.ID).Scan(&got)
	if got != "@ali212" {
		t.Fatalf("label %q", got)
	}
	var kid string
	var kidNeed int
	if err := s.api.store.DB().QueryRow(`SELECT id,need FROM admin_actions WHERE parent=?`, c.ID).Scan(&kid, &kidNeed); err != nil || kidNeed != 1 {
		t.Fatalf("companion card for the other phone: %v need %d", err, kidNeed)
	}
	res := doJSON(t, s.api.AdminActionsList, http.MethodGet, "/api/v1/admin/actions?state=pending", nil,
		map[string]string{"Authorization": "Bearer " + s.admin.Token})
	var list struct {
		Actions []adminActionRow `json:"actions"`
		Pending int              `json:"pending"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &list)
	if res.Code != http.StatusOK || list.Pending != 1 || len(list.Actions) != 1 || list.Actions[0].Need != 2 || !list.Actions[0].Offline {
		t.Fatalf("list %d %s", res.Code, res.Body.String())
	}
}

func TestRolesAndFakeCards220(t *testing.T) {
	if adminRoleCan("operator", "block") || !adminRoleCan("operator", "wifi_rule") || !adminRoleCan("operator", "remove_phone") {
		t.Fatal("operator: wifi + remove phone only")
	}
	if adminRoleCan("operator", "sign_out") || adminRoleCan("security", "wifi_rule") || adminRoleCan("auditor", "block") {
		t.Fatal("sign out is owner only; security / auditor make no actions")
	}
	if adminRoleCan("operator", "version_rule") || adminRoleCan("operator", "admin_badge") || !adminRoleCan("owner", "admin_badge") {
		t.Fatal("version rule and admin badge are owner only")
	}
	s := setup212(t)
	card := s.actionCard220(t, "sign_out", 1, nil, "")
	card.Body = strings.Replace(card.Body, `"need":1`, `"need":0`, 1)
	if _, reason := s.api.checkAdminAction(card); reason != "bad_sig" {
		t.Fatalf("changed card: %s", reason)
	}
	big := s.actionCard220(t, "sign_out", 1, nil, "")
	big.Body += strings.Repeat(" ", maxAdminActionCardBytes)
	if _, reason := s.api.checkAdminAction(big); reason != "too_big" {
		t.Fatalf("big card: %s", reason)
	}
	bad := s.actionCard220(t, "format_phone", 1, nil, "")
	if _, reason := s.api.checkAdminAction(bad); reason != "bad_fields" {
		t.Fatalf("unknown action: %s", reason)
	}
}

func cancel220(t *testing.T, s *blocks212, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/actions/"+id+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+s.admin.Token)
	req.RemoteAddr = "10.21.2.1:1234"
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	s.api.AdminActionCancel(rec, req)
	return rec
}

func TestCancelStopsTheCard220(t *testing.T) {
	s := setup212(t)
	addPhoneKeys220(t, s.api, s.ali)
	setStatus220(t, s, s.ali.Account.ID, "suspended")
	id, _, _ := lastAction220(t, s.api, "block")
	if res := cancel220(t, s, id); res.Code != http.StatusOK {
		t.Fatalf("cancel %d %s", res.Code, res.Body.String())
	}
	if state, _, _ := actionState220(s.api, id); state != "cancelled" {
		t.Fatalf("state %s", state)
	}
	_, c, _ := lastAction220(t, s.api, "cancel")
	if c.Cancels != id || c.Signer != "server" {
		t.Fatalf("cancel card %+v", c)
	}
	out := sync220(t, s.api, s.bilal, map[string]any{"have": []string{id}})
	if len(out.States) != 1 || !strings.Contains(out.States[0].Body, `"cancelled"`) {
		t.Fatalf("cancel stop note: %+v", out.States)
	}
	if res := cancel220(t, s, id); res.Code != http.StatusConflict {
		t.Fatalf("second cancel must be 409, got %d", res.Code)
	}
	// An offline cancel made on the admin phone works too.
	setStatus220(t, s, s.ali.Account.ID, "active")
	id2, _, _ := lastAction220(t, s.api, "unblock")
	sync220(t, s.api, s.bilal, map[string]any{"cards": []signedBlob{s.actionCard220(t, "cancel", 0, nil, id2)}})
	if state, _, _ := actionState220(s.api, id2); state != "cancelled" {
		t.Fatalf("offline cancel: %s", state)
	}
}

func TestVersionRuleReachedAndReplaced220(t *testing.T) {
	s := setup212(t)
	p := addPhoneKeys220(t, s.api, s.bilal)
	admin := AdminAccount{ID: "a1", Username: "test-admin", Role: "owner"}
	s.api.adminActionForVersionRule(versionRuleBody{Mode: "warn", MinVersion: "3.0.60"}, signedBlob{Body: "{}", Sig: "x"}, admin)
	id1, c, _ := lastAction220(t, s.api, "version_rule")
	if c.Need != 0 || len(c.To) != 0 {
		t.Fatalf("version card has no boxes: %+v", c)
	}
	sync220(t, s.api, s.bilal, map[string]any{"signing_key": p.signingKey(), "receipts": []signedBlob{p.done(id1, "ok")}})
	if state, _, done := actionState220(s.api, id1); state != "pending" || done != 1 {
		t.Fatalf("version rule stays pending and counts reached: %s %d", state, done)
	}
	time.Sleep(2 * time.Millisecond)
	s.api.adminActionForVersionRule(versionRuleBody{Mode: "stop_now", MinVersion: "3.0.61"}, signedBlob{Body: "{}", Sig: "y"}, admin)
	if state, _, _ := actionState220(s.api, id1); state != "replaced" {
		t.Fatalf("older rule card must be replaced: %s", state)
	}
}

func TestSignOutAndRemovePhoneCards220(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	var sessionID string
	_ = s.api.store.DB().QueryRow(`SELECT id FROM auth_sessions WHERE device_id=? AND revoked_at IS NULL`, s.ali.Device.ID).Scan(&sessionID)
	res := doJSON(t, s.api.AdminRevokeSession, http.MethodPost, "/api/v1/admin/session/revoke?id="+sessionID, nil,
		map[string]string{"Authorization": "Bearer " + s.admin.Token})
	if res.Code != http.StatusOK {
		t.Fatalf("revoke session %d %s", res.Code, res.Body.String())
	}
	_, c, _ := lastAction220(t, s.api, "sign_out")
	if got := openMine220(t, ali, c); got.Detail["session_id"] != sessionID {
		t.Fatalf("sign out box: %+v", got)
	}
	res = doJSON(t, s.api.AdminRevokeDevice, http.MethodPost, "/api/v1/admin/device/revoke?id="+s.ali.Device.ID, nil,
		map[string]string{"Authorization": "Bearer " + s.admin.Token})
	if res.Code != http.StatusOK {
		t.Fatalf("revoke device %d %s", res.Code, res.Body.String())
	}
	_, c, _ = lastAction220(t, s.api, "remove_phone")
	if c.Need != 1 {
		t.Fatalf("a removed phone (retired key) still gets a box: %+v", c)
	}
	openMine220(t, ali, c)
}

func TestPendingCapOldAppAndVia220(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	// Old app on Ali's phone -> "Pending · old app".
	_, _ = s.api.store.DB().Exec(`INSERT INTO device_builds(device_id,build,seen_at) VALUES(?,?,?)`, s.ali.Device.ID, 130, nowStamp())
	setStatus220(t, s, s.ali.Account.ID, "suspended")
	id, _, _ := lastAction220(t, s.api, "block")
	row, ok := s.api.adminActionRowFor(id)
	if !ok || row.OldApp != 1 || len(row.Phones) != 1 || !row.Phones[0].OldApp {
		t.Fatalf("old app flag: %+v", row)
	}
	// The target's own Done is trusted by its sign-in and says how the card came.
	raw, _ := json.Marshal(adminActionDoneBody{V: 1, Kind: "admin_action_done", ActionID: id, Device: s.ali.Device.ID,
		User: s.ali.Account.ID, Result: "ok", At: nowStamp(), Build: 131, Via: "wifi_direct"})
	done := signedBlob{Body: string(raw), Sig: b64url(ed25519.Sign(ali.sign, raw))}
	doJSON(t, s.api.AdminActionsMine, http.MethodPost, "/api/v1/admin-actions/mine", map[string]any{"receipts": []signedBlob{done}}, directoryHeaders(s.ali))
	row, _ = s.api.adminActionRowFor(id)
	if row.State != "done" || row.Via != "wifi_direct" || row.OldApp != 0 {
		t.Fatalf("via wifi_direct: %+v", row)
	}
	// 50 pending per admin: the 51st offline card is refused and named.
	for i := 0; i < maxPendingPerAdmin; i++ {
		sync220(t, s.api, s.bilal, map[string]any{"cards": []signedBlob{s.actionCard220(t, "sign_out", 1, nil, "")}})
	}
	extra := s.actionCard220(t, "sign_out", 1, nil, "")
	var c adminActionBody
	_ = json.Unmarshal([]byte(extra.Body), &c)
	res := doJSON(t, s.api.AdminActionsSync, http.MethodPost, "/api/v1/admin-actions/sync", map[string]any{"cards": []signedBlob{extra}}, directoryHeaders(s.bilal))
	var out struct {
		TooMany []string `json:"too_many_pending"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if len(out.TooMany) != 1 || out.TooMany[0] != c.ID {
		t.Fatalf("cap: %s", res.Body.String())
	}
	// The first signing key is kept; a different later key is only logged.
	other := phone220{sign: func() ed25519.PrivateKey { _, k, _ := ed25519.GenerateKey(rand.Reader); return k }()}
	s.api.rememberSigningKey(s.bilal.Device.ID, ali.signingKey())
	s.api.rememberSigningKey(s.bilal.Device.ID, other.signingKey())
	var key string
	_ = s.api.store.DB().QueryRow(`SELECT signing_key FROM device_signing_keys WHERE device_id=?`, s.bilal.Device.ID).Scan(&key)
	var logged int
	_ = s.api.store.DB().QueryRow(`SELECT COUNT(*) FROM security_audit WHERE action='SIGNING_KEY_CHANGED'`).Scan(&logged)
	if key != ali.signingKey() || logged != 1 {
		t.Fatalf("first key kept: %v logged %d", key == ali.signingKey(), logged)
	}
}
