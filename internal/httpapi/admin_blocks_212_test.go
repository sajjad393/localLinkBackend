package httpapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Bundle 212: offline admin blocks (server key, admin offline key, signed
// records, sync, newest wins, keys end on sign out).

type blocks212 struct {
	api      *API
	admin    AdminLoginResponse
	adminDev AuthResponse
	ali      AuthResponse
	bilal    AuthResponse
	phoneKey ed25519.PrivateKey
	cert     signedBlob
}

func setup212(t *testing.T) *blocks212 {
	t.Helper()
	_, api := newTestAPI(t)
	if err := api.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	s := &blocks212{api: api}
	s.admin = adminLogin211(t, api, "10.21.2.1")
	s.adminDev = registerDirectoryUser(t, api, "sajjad212", "dev-admin-212")
	s.ali = registerDirectoryUser(t, api, "ali212", "dev-ali-212")
	s.bilal = registerDirectoryUser(t, api, "bilal212", "dev-bilal-212")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s.phoneKey = priv
	res := doJSON(t, api.AdminOfflineCert, http.MethodPost, "/api/v1/admin/offline-cert",
		map[string]string{"device_id": "dev-admin-212", "signing_key": b64url(pub)},
		map[string]string{"Authorization": "Bearer " + s.admin.Token})
	if res.Code != http.StatusOK {
		t.Fatalf("offline-cert %d %s", res.Code, res.Body.String())
	}
	var out struct {
		Cert      signedBlob `json:"cert"`
		ServerKey string     `json:"server_key"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !verifyWithKey(out.ServerKey, out.Cert) {
		t.Fatal("cert must verify with the server key")
	}
	s.cert = out.Cert
	return s
}

func (s *blocks212) record(t *testing.T, target, action string, seq int64, devices ...string) signedBlob {
	t.Helper()
	return s.recordAt(t, target, action, seq, time.Now().UTC(), devices...)
}

func (s *blocks212) recordAt(t *testing.T, target, action string, seq int64, at time.Time, devices ...string) signedBlob {
	t.Helper()
	cert := s.cert
	rec := adminBlockBody{V: 1, Kind: "admin_block", ID: uuid.NewString(), TargetUserID: target, TargetDevices: devices,
		TargetName: "x", Action: action, Seq: seq, At: at.Format(time.RFC3339Nano), By: "test-admin",
		Signer: "admin", Cert: &cert}
	raw, _ := json.Marshal(rec)
	return signedBlob{Body: string(raw), Sig: b64url(ed25519.Sign(s.phoneKey, raw))}
}

type sync212 struct {
	Records  []signedBlob `json:"records"`
	Ended    []signedBlob `json:"ended"`
	Applied  int          `json:"applied"`
	Rejected int          `json:"rejected"`
}

func (s *blocks212) sync(t *testing.T, from AuthResponse, records ...signedBlob) (int, sync212) {
	t.Helper()
	if records == nil {
		records = []signedBlob{}
	}
	res := doJSON(t, s.api.AdminBlocksSync, http.MethodPost, "/api/v1/admin-blocks/sync",
		map[string]any{"records": records}, directoryHeaders(from))
	var out sync212
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out
}

func (s *blocks212) status(t *testing.T, userID string) string {
	t.Helper()
	var st string
	if err := s.api.store.DB().QueryRow(`SELECT status FROM users WHERE id=?`, userID).Scan(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func (s *blocks212) liveSessions(t *testing.T, userID string) int {
	t.Helper()
	var n int
	_ = s.api.store.DB().QueryRow(`SELECT COUNT(*) FROM auth_sessions WHERE user_id=? AND revoked_at IS NULL`, userID).Scan(&n)
	return n
}

func TestTrustKeyIsStable212(t *testing.T) {
	_, api := newTestAPI(t)
	get := func() string {
		res := doJSON(t, api.Trust, http.MethodGet, "/api/v1/trust", nil)
		if res.Code != http.StatusOK {
			t.Fatalf("trust %d", res.Code)
		}
		var out map[string]string
		_ = json.Unmarshal(res.Body.Bytes(), &out)
		if len(out["server_key"]) < 40 || out["key_id"] == "" {
			t.Fatalf("bad trust %v", out)
		}
		return out["server_key"]
	}
	if a, b := get(), get(); a != b {
		t.Fatal("server key must not change between calls")
	}
}

func TestOfflineBlockSyncsAndUnblockRestores212(t *testing.T) {
	s := setup212(t)
	aliID := s.ali.Account.ID
	code, out := s.sync(t, s.bilal, s.record(t, aliID, "block", 1, "dev-ali-212"))
	if code != http.StatusOK || out.Applied != 1 {
		t.Fatalf("sync %d %+v", code, out)
	}
	if st := s.status(t, aliID); st != "suspended" {
		t.Fatalf("ali status %s", st)
	}
	if s.liveSessions(t, aliID) == 0 {
		t.Fatal("a block must not end Ali's sign-in")
	}
	// Ali's phone can't use normal calls, but can read its own record.
	if code, _ := s.sync(t, s.ali); code != http.StatusUnauthorized {
		t.Fatalf("blocked phone sync want 401 got %d", code)
	}
	res := doJSON(t, s.api.AdminBlocksMine, http.MethodPost, "/api/v1/admin-blocks/mine", nil, directoryHeaders(s.ali))
	if res.Code != http.StatusOK {
		t.Fatalf("mine %d", res.Code)
	}
	var mine struct {
		Record *signedBlob `json:"record"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &mine)
	if mine.Record == nil {
		t.Fatal("blocked phone must get its own record")
	}
	// Older seq is ignored; newer unblock wins and Ali works again.
	if _, out := s.sync(t, s.bilal, s.recordAt(t, aliID, "unblock", 1, time.Now().UTC().Add(-time.Hour))); out.Applied != 0 {
		t.Fatal("same seq with an older time must not win")
	}
	if _, out := s.sync(t, s.bilal, s.record(t, aliID, "unblock", 2)); out.Applied != 1 {
		t.Fatal("newer unblock must win")
	}
	if st := s.status(t, aliID); st != "active" {
		t.Fatalf("after unblock %s", st)
	}
	if code, _ := s.sync(t, s.ali); code != http.StatusOK {
		t.Fatalf("unblocked phone works again without a new sign-in, got %d", code)
	}
}

func TestFakeAndSelfBlocksRejected212(t *testing.T) {
	s := setup212(t)
	rec := s.record(t, s.ali.Account.ID, "block", 1)
	rec.Body = rec.Body[:len(rec.Body)-1] + " }"
	if _, out := s.sync(t, s.bilal, rec); out.Rejected != 1 || out.Applied != 0 {
		t.Fatalf("changed record must be rejected %+v", out)
	}
	// Signed by a key the server never certified.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	good := s.record(t, s.ali.Account.ID, "block", 1)
	good.Sig = b64url(ed25519.Sign(other, []byte(good.Body)))
	if _, out := s.sync(t, s.bilal, good); out.Rejected != 1 {
		t.Fatal("wrong key must be rejected")
	}
	// The admin's own account.
	if _, out := s.sync(t, s.bilal, s.record(t, s.adminDev.Account.ID, "block", 1)); out.Rejected != 1 {
		t.Fatal("self block must be rejected")
	}
	if st := s.status(t, s.adminDev.Account.ID); st != "active" {
		t.Fatal("admin account must stay active")
	}
}

func TestSignOutEndsOfflineKey212(t *testing.T) {
	s := setup212(t)
	res := doJSON(t, s.api.AdminLogout, http.MethodPost, "/api/v1/admin/logout", nil,
		map[string]string{"Authorization": "Bearer " + s.admin.Token})
	if res.Code != http.StatusOK {
		t.Fatalf("logout %d", res.Code)
	}
	_, out := s.sync(t, s.bilal, s.record(t, s.ali.Account.ID, "block", 1))
	if out.Rejected != 1 {
		t.Fatal("a record from an ended key must be rejected")
	}
	if len(out.Ended) != 1 || !s.api.serverVerify(out.Ended[0]) {
		t.Fatalf("one signed key-ended note expected, got %d", len(out.Ended))
	}
}

func TestWebSuspendIsSignedAndKeepsSessions212(t *testing.T) {
	s := setup212(t)
	aliID := s.ali.Account.ID
	set := func(status string) {
		res := doJSON(t, s.api.AdminSetUserStatus, http.MethodPost, "/api/v1/admin/user/status?id="+aliID+"&status="+status, nil,
			map[string]string{"Authorization": "Bearer " + s.admin.Token})
		if res.Code != http.StatusOK {
			t.Fatalf("status %s: %d %s", status, res.Code, res.Body.String())
		}
	}
	set("suspended")
	if s.liveSessions(t, aliID) == 0 {
		t.Fatal("suspend must keep the sign-in")
	}
	_, out := s.sync(t, s.bilal)
	if len(out.Records) != 1 {
		t.Fatalf("one server record expected, got %d", len(out.Records))
	}
	rec, reason := s.api.checkAdminBlock(out.Records[0])
	if reason != "" || rec.Signer != "server" || rec.Action != "block" || rec.Seq != 1 {
		t.Fatalf("bad server record %+v %s", rec, reason)
	}
	set("active")
	_, out = s.sync(t, s.bilal)
	rec, _ = s.api.checkAdminBlock(out.Records[0])
	if rec.Action != "unblock" || rec.Seq != 2 {
		t.Fatalf("unsuspend must be seq 2 unblock, got %+v", rec)
	}
	set("disabled")
	if s.liveSessions(t, aliID) != 0 {
		t.Fatal("disabled still ends every sign-in")
	}
}
