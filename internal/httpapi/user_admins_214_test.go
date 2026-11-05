package httpapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Bundle 214: app users as admins (make / remove, device admin session,
// signed admin cards, web login refused, ends with the account).

type admins214 struct {
	api   *API
	owner AdminLoginResponse
	saj   AuthResponse
	ali   AuthResponse
}

func setup214(t *testing.T) *admins214 {
	t.Helper()
	_, api := newTestAPI(t)
	if err := api.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	s := &admins214{api: api}
	s.owner = adminLogin211(t, api, "10.21.4.1")
	s.saj = registerDirectoryUser(t, api, "sajjad214", "dev-saj-214")
	s.ali = registerDirectoryUser(t, api, "ali214", "dev-ali-214")
	return s
}

func (s *admins214) setAdmin(t *testing.T, token string, body map[string]any) (int, string) {
	t.Helper()
	res := doJSON(t, s.api.AdminSetUserAdmin, http.MethodPost, "/api/v1/admin/user-admin", body,
		map[string]string{"Authorization": "Bearer " + token})
	return res.Code, res.Body.String()
}

type devSession214 struct {
	Admin     AdminAccount `json:"admin"`
	Token     string       `json:"token"`
	ExpiresAt string       `json:"expires_at"`
	Card      *signedBlob  `json:"card"`
}

func (s *admins214) deviceSession(t *testing.T, who AuthResponse, token string) (int, devSession214) {
	t.Helper()
	body := map[string]any{}
	if token != "" {
		body["token"] = token
	}
	res := doJSON(t, s.api.AdminDeviceSession, http.MethodPost, "/api/v1/admin/device-session", body, directoryHeaders(who))
	var out devSession214
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if res.Code == http.StatusForbidden && !strings.Contains(res.Body.String(), "not_admin") {
		t.Fatalf("403 must say not_admin: %s", res.Body.String())
	}
	return res.Code, out
}

func badge214(t *testing.T, blob signedBlob) adminBadgeBody {
	t.Helper()
	var b adminBadgeBody
	if err := json.Unmarshal([]byte(blob.Body), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func (s *admins214) usersRole(t *testing.T, userID string) string {
	t.Helper()
	res := doJSON(t, s.api.AdminUsers, http.MethodGet, "/api/v1/admin/users?limit=200", nil,
		map[string]string{"Authorization": "Bearer " + s.owner.Token})
	var out struct {
		Users []AdminUser `json:"users"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	for _, u := range out.Users {
		if u.ID == userID {
			return u.AdminRole
		}
	}
	t.Fatalf("user %s not listed", userID)
	return ""
}

func (s *admins214) offlineCert(t *testing.T, token, deviceID string) (int, signedBlob) {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	return s.offlineCertKey(t, token, deviceID, pub)
}

func (s *admins214) offlineCertKey(t *testing.T, token, deviceID string, pub ed25519.PublicKey) (int, signedBlob) {
	t.Helper()
	res := doJSON(t, s.api.AdminOfflineCert, http.MethodPost, "/api/v1/admin/offline-cert",
		map[string]string{"device_id": deviceID, "signing_key": b64url(pub)},
		map[string]string{"Authorization": "Bearer " + token})
	var out struct {
		Cert signedBlob `json:"cert"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out.Cert
}

func TestUserAdminMakeGivesDeviceSessionAndSignedCard(t *testing.T) {
	s := setup214(t)
	if code, _ := s.deviceSession(t, s.saj, ""); code != http.StatusForbidden {
		t.Fatalf("not admin yet must be 403, got %d", code)
	}
	// Owner is the default level.
	if code, body := s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID}); code != http.StatusOK {
		t.Fatalf("make admin %d %s", code, body)
	}
	if role := s.usersRole(t, s.saj.Account.ID); role != "owner" {
		t.Fatalf("users list admin_role = %q", role)
	}
	if role := s.usersRole(t, s.ali.Account.ID); role != "" {
		t.Fatalf("normal user admin_role = %q", role)
	}
	code, sess := s.deviceSession(t, s.saj, "")
	if code != http.StatusOK || sess.Token == "" || sess.Admin.Role != "owner" || sess.Admin.Username != "sajjad214" {
		t.Fatalf("device session %d %+v", code, sess)
	}
	if sess.Card == nil || !s.api.serverVerify(*sess.Card) {
		t.Fatal("card must be server signed")
	}
	card := badge214(t, *sess.Card)
	if card.Kind != "admin_badge" || !card.On || card.Role != "owner" || card.UserID != s.saj.Account.ID ||
		card.Username != "sajjad214" || card.Signer != "server" || card.Seq < 1 {
		t.Fatalf("card %+v", card)
	}
	// The admin token works like a web sign-in (read + owner tools + 212 key).
	if c := adminSummary211(t, s.api, sess.Token); c != http.StatusOK {
		t.Fatalf("summary with device admin token: %d", c)
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	c, cert := s.offlineCertKey(t, sess.Token, "dev-saj-214", pub)
	if c != http.StatusOK {
		t.Fatalf("offline cert %d", c)
	}
	var cb adminCertBody
	_ = json.Unmarshal([]byte(cert.Body), &cb)
	if cb.Role != "owner" || cb.Username != "sajjad214" {
		t.Fatalf("cert %+v", cb)
	}
	// Refresh with the token: same token. Without it: new token, same
	// session row, so the offline key stays the same.
	_, again := s.deviceSession(t, s.saj, sess.Token)
	if again.Token != sess.Token {
		t.Fatal("refresh with a live token must keep it")
	}
	_, fresh := s.deviceSession(t, s.saj, "")
	if fresh.Token == sess.Token {
		t.Fatal("refresh without token must rotate")
	}
	if c := adminSummary211(t, s.api, sess.Token); c != http.StatusUnauthorized {
		t.Fatalf("old token must stop after rotate: %d", c)
	}
	_, cert2 := s.offlineCertKey(t, fresh.Token, "dev-saj-214", pub)
	if cert2.Body != cert.Body {
		t.Fatal("same session + same phone key must give the same offline key")
	}
	// Badges list for phones (device auth).
	res := doJSON(t, s.api.AdminBadges, http.MethodGet, "/api/v1/admin-badges", nil, directoryHeaders(s.ali))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `\"user_id\":\"`+s.saj.Account.ID) {
		t.Fatalf("badges %d %s", res.Code, res.Body.String())
	}
	if res := doJSON(t, s.api.AdminBadges, http.MethodGet, "/api/v1/admin-badges", nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("badges need device auth: %d", res.Code)
	}
}

func TestUserAdminWebLoginRefusedAndOnlyWebOwnerMakesAdmins(t *testing.T) {
	s := setup214(t)
	if code, body := s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID, "role": "owner"}); code != http.StatusOK {
		t.Fatalf("make %d %s", code, body)
	}
	// The linked admin account can't sign in on the web.
	res := doJSON(t, s.api.AdminLogin, http.MethodPost, "/api/v1/admin/login",
		map[string]string{"username": "app:" + s.saj.Account.ID, "password": "whatever-password-1"},
		map[string]string{"X-Forwarded-For": "10.21.4.9"})
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("linked admin web login must fail: %d", res.Code)
	}
	// An app admin (even Owner) can't make admins.
	_, sess := s.deviceSession(t, s.saj, "")
	if code, _ := s.setAdmin(t, sess.Token, map[string]any{"user_id": s.ali.Account.ID}); code != http.StatusForbidden {
		t.Fatalf("app admin making admins must be 403, got %d", code)
	}
	// Bad role / unknown user.
	if code, _ := s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.ali.Account.ID, "role": "king"}); code != http.StatusBadRequest {
		t.Fatalf("bad role: %d", code)
	}
	if code, _ := s.setAdmin(t, s.owner.Token, map[string]any{"user_id": "nobody"}); code != http.StatusNotFound {
		t.Fatalf("unknown user: %d", code)
	}
}

func TestUserAdminLevelChangeAndRemoveEndEverything(t *testing.T) {
	s := setup214(t)
	s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID})
	_, sess := s.deviceSession(t, s.saj, "")
	c, cert := s.offlineCert(t, sess.Token, "dev-saj-214")
	if c != http.StatusOK {
		t.Fatalf("cert %d", c)
	}
	var cb adminCertBody
	_ = json.Unmarshal([]byte(cert.Body), &cb)

	// Level -> operator: new card, old offline key ended, owner tools refused.
	if code, body := s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID, "role": "operator"}); code != http.StatusOK {
		t.Fatalf("level %d %s", code, body)
	}
	_, sess2 := s.deviceSession(t, s.saj, sess.Token)
	if sess2.Admin.Role != "operator" || badge214(t, *sess2.Card).Role != "operator" {
		t.Fatalf("level not applied: %+v", sess2)
	}
	if !strings.Contains(notes214(s.api), cb.CertID) {
		t.Fatal("level change must end the old offline key")
	}
	if c, _ := s.offlineCert(t, sess2.Token, "dev-saj-214"); c != http.StatusForbidden {
		t.Fatalf("operator must not get the owner offline key: %d", c)
	}
	// Back to owner, new key, then remove.
	s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID, "role": "owner"})
	_, sess3 := s.deviceSession(t, s.saj, sess2.Token)
	_, cert3 := s.offlineCert(t, sess3.Token, "dev-saj-214")
	var cb3 adminCertBody
	_ = json.Unmarshal([]byte(cert3.Body), &cb3)
	if code, body := s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID, "remove": true}); code != http.StatusOK {
		t.Fatalf("remove %d %s", code, body)
	}
	if c := adminSummary211(t, s.api, sess3.Token); c != http.StatusUnauthorized {
		t.Fatalf("removed admin token must stop: %d", c)
	}
	if !strings.Contains(notes214(s.api), cb3.CertID) {
		t.Fatal("remove must end the offline key")
	}
	if code, _ := s.deviceSession(t, s.saj, sess3.Token); code != http.StatusForbidden {
		t.Fatalf("removed admin device session: %d", code)
	}
	card, _ := s.api.currentBadge(s.saj.Account.ID)
	if card == nil || card.On {
		t.Fatalf("remove must save a card with on:false: %+v", card)
	}
	if role := s.usersRole(t, s.saj.Account.ID); role != "" {
		t.Fatalf("removed admin still listed: %q", role)
	}
	// Removing again: 404. Make again: works, newer card.
	if code, _ := s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID, "remove": true}); code != http.StatusNotFound {
		t.Fatalf("remove twice: %d", code)
	}
	s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID})
	again, _ := s.api.currentBadge(s.saj.Account.ID)
	if again == nil || !again.On || again.Seq <= card.Seq {
		t.Fatalf("make again must give a newer card: %+v", again)
	}
}

func notes214(api *API) string {
	var b strings.Builder
	for _, n := range api.certEndedNotes() {
		b.WriteString(n.Body)
	}
	return b.String()
}

func TestUserAdminEndsWithLogoutRevokeDisableAndSuspend(t *testing.T) {
	s := setup214(t)
	s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID})
	_, sess := s.deviceSession(t, s.saj, "")

	// Suspended: role stays, admin tools refused, back after unsuspend.
	setStatus := func(status string) {
		res := doJSON(t, s.api.AdminSetUserStatus, http.MethodPost,
			"/api/v1/admin/user/status?id="+s.saj.Account.ID+"&status="+status, nil,
			map[string]string{"Authorization": "Bearer " + s.owner.Token})
		if res.Code != http.StatusOK {
			t.Fatalf("status %s: %d %s", status, res.Code, res.Body.String())
		}
	}
	setStatus("suspended")
	if c := adminSummary211(t, s.api, sess.Token); c != http.StatusUnauthorized {
		t.Fatalf("suspended admin tools must stop: %d", c)
	}
	if role := s.usersRole(t, s.saj.Account.ID); role != "owner" {
		t.Fatalf("suspend keeps the role: %q", role)
	}
	setStatus("active")
	if c := adminSummary211(t, s.api, sess.Token); c != http.StatusOK {
		t.Fatalf("unsuspended admin back: %d", c)
	}

	// Phone logout ends that phone's admin session.
	res := doJSON(t, s.api.LogoutAccount, http.MethodPost, "/api/v1/auth/logout", nil, directoryHeaders(s.saj))
	if res.Code != http.StatusOK {
		t.Fatalf("logout %d", res.Code)
	}
	if c := adminSummary211(t, s.api, sess.Token); c != http.StatusUnauthorized {
		t.Fatalf("logged out phone admin token must stop: %d", c)
	}
	var revoked int
	_ = s.api.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_sessions WHERE device_id='dev-saj-214' AND revoked_at IS NOT NULL`).Scan(&revoked)
	if revoked == 0 {
		t.Fatal("logout must revoke the device admin session")
	}

	// Disable removes admin.
	s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.ali.Account.ID, "role": "security"})
	res = doJSON(t, s.api.AdminSetUserStatus, http.MethodPost,
		"/api/v1/admin/user/status?id="+s.ali.Account.ID+"&status=disabled", nil,
		map[string]string{"Authorization": "Bearer " + s.owner.Token})
	if res.Code != http.StatusOK {
		t.Fatalf("disable %d", res.Code)
	}
	if role := s.usersRole(t, s.ali.Account.ID); role != "" {
		t.Fatalf("disabled user must not be admin: %q", role)
	}
	if code, _ := s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.ali.Account.ID}); code != http.StatusConflict {
		t.Fatalf("disabled user can't be made admin: %d", code)
	}
}

func TestUserAdminLastOwnerRefused(t *testing.T) {
	s := setup214(t)
	s.setAdmin(t, s.owner.Token, map[string]any{"user_id": s.saj.Account.ID})
	// Only the app owner is left (the web owner is switched off); the test
	// static token acts as the web owner.
	t.Setenv("LOCALLINK_ADMIN_TOKEN", "static-214-token")
	if _, err := s.api.store.DB().Exec(`UPDATE admin_accounts SET status='disabled' WHERE username='test-admin'`); err != nil {
		t.Fatal(err)
	}
	call := func(body map[string]any) int {
		return doJSON(t, s.api.AdminSetUserAdmin, http.MethodPost, "/api/v1/admin/user-admin", body,
			map[string]string{"X-Admin-Token": "static-214-token"}).Code
	}
	if c := call(map[string]any{"user_id": s.saj.Account.ID, "remove": true}); c != http.StatusConflict {
		t.Fatalf("removing the last owner must be refused: %d", c)
	}
	if c := call(map[string]any{"user_id": s.saj.Account.ID, "role": "auditor"}); c != http.StatusConflict {
		t.Fatalf("demoting the last owner must be refused: %d", c)
	}
	// With a second owner it works.
	if c := call(map[string]any{"user_id": s.ali.Account.ID}); c != http.StatusOK {
		t.Fatalf("make second owner: %d", c)
	}
	if c := call(map[string]any{"user_id": s.saj.Account.ID, "remove": true}); c != http.StatusOK {
		t.Fatalf("remove with another owner: %d", c)
	}
}

func TestUserAdminWebUsersPageHasAdminColumnAndButtons(t *testing.T) {
	views, err := adminWebFS.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	app, err := adminWebFS.ReadFile("adminweb/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"{ label: 'Admin' }", "data-user-admin=", "data-admin-mode=\"make\"",
		"data-admin-mode=\"remove\"", "data-admin-mode=\"level\"", "key: 'admin'", "app admins"} {
		if !strings.Contains(string(views), want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
	for _, want := range []string{"/api/v1/admin/user-admin", "adminLevelDialog", "Owner (default)"[:5], "Remove admin"} {
		if !strings.Contains(string(app), want) {
			t.Fatalf("app.js misses %q", want)
		}
	}
}

func TestUserAdminMigration57Tables(t *testing.T) {
	st, _ := newTestAPI(t)
	for _, q := range []string{
		`SELECT user_id FROM admin_accounts LIMIT 1`,
		`SELECT device_id FROM admin_sessions LIMIT 1`,
		`SELECT user_id,record_id,role,on_flag,seq,at,body,sig,updated_at FROM admin_badges LIMIT 1`,
	} {
		rows, err := st.DB().Query(q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		rows.Close()
	}
	var n int
	_ = st.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=57`).Scan(&n)
	if n != 1 {
		t.Fatal("migration 57 must be applied")
	}
}
