package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Bundle 222: Forgot password with the recovery code (online + offline sync),
// the no-code request to the admin, and the admin list kinds.

func newAPI222(t *testing.T) *API {
	t.Helper()
	t.Setenv("LOCALLINK_ADMIN_TOKEN", "admin-secret")
	_, a := newTestAPI(t)
	return a
}

func register222(t *testing.T, a *API, user, pass, device string) AuthResponse {
	t.Helper()
	res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": user, "password": pass, "device_id": device, "device_name": "Phone " + device, "platform": "android",
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("register %s: %d %s", user, res.Code, res.Body.String())
	}
	var out AuthResponse
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return out
}

func login222(t *testing.T, a *API, user, pass, device string) (int, AuthResponse) {
	t.Helper()
	res := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": user, "password": pass, "device_id": device, "device_name": "Phone " + device, "platform": "android",
	})
	var out AuthResponse
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out
}

func signedIn222(a *API, device, token string) bool {
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Device-ID", device)
	req.Header.Set("Authorization", "Bearer "+token)
	_, ok := a.authorizeDevice(req)
	return ok
}

func TestResetWithCodeOnline222(t *testing.T) {
	a := newAPI222(t)
	first := register222(t, a, "sumaira", "old-password-1", "phone-a")
	code, other := login222(t, a, "sumaira", "old-password-1", "phone-b")
	if code != http.StatusOK {
		t.Fatalf("login phone-b: %d", code)
	}
	// Spaces / lower case in the typed code are fine.
	typed := strings.ToLower(strings.ReplaceAll(first.RecoveryCode, "-", " "))
	res := doJSON(t, a.ResetPasswordWithCode222, http.MethodPost, "/api/v1/account/recovery/reset", map[string]any{
		"username": "Sumaira", "recovery_code": typed, "new_password": "new-password-2",
		"device_id": "phone-a", "device_name": "Phone A", "platform": "android",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", res.Code, res.Body.String())
	}
	var out AuthResponse
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if out.Token == "" || out.Account.Username != "sumaira" || out.Device.ID != "phone-a" {
		t.Fatalf("bad reset answer: %+v", out)
	}
	if !recoveryFormat218.MatchString(out.RecoveryCode) || out.RecoveryCode == first.RecoveryCode {
		t.Fatalf("a NEW recovery code must be shown once: %q (old %q)", out.RecoveryCode, first.RecoveryCode)
	}
	if !signedIn222(a, "phone-a", out.Token) {
		t.Fatal("this phone must be signed in with the new token")
	}
	if !signedIn222(a, "phone-b", other.Token) {
		t.Fatal("the other phone must stay signed in (user's choice)")
	}
	if c, _ := login222(t, a, "sumaira", "old-password-1", "phone-c"); c != http.StatusUnauthorized {
		t.Fatalf("old password must fail: %d", c)
	}
	if c, _ := login222(t, a, "sumaira", "new-password-2", "phone-c"); c != http.StatusOK {
		t.Fatalf("new password must work on another phone: %d", c)
	}
	// The old code is used up; the new one works.
	again := doJSON(t, a.ResetPasswordWithCode222, http.MethodPost, "/api/v1/account/recovery/reset", map[string]any{
		"username": "sumaira", "recovery_code": first.RecoveryCode, "new_password": "new-password-3",
		"device_id": "phone-a", "device_name": "Phone A",
	})
	if again.Code != http.StatusUnauthorized || !strings.Contains(again.Body.String(), "BAD_RECOVERY_CODE") {
		t.Fatalf("old code must not work again: %d %s", again.Code, again.Body.String())
	}
	// The Owner sees it in Recovery as "reset_code" (audit only).
	list := doJSON(t, a.AdminRecoveryRequests, http.MethodGet, "/api/v1/admin/recovery-requests?limit=50", nil, map[string]string{"X-Admin-Token": "admin-secret"})
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"kind":"reset_code"`) || !strings.Contains(list.Body.String(), `"status":"completed"`) {
		t.Fatalf("admin list must show the reset: %d %s", list.Code, list.Body.String())
	}
}

func TestResetWithCodeWrongAndLimit222(t *testing.T) {
	a := newAPI222(t)
	register222(t, a, "ali", "ali-password-1", "ali-phone")
	var last int
	for i := 0; i < 6; i++ {
		res := doJSON(t, a.ResetPasswordWithCode222, http.MethodPost, "/api/v1/account/recovery/reset", map[string]any{
			"username": "ali", "recovery_code": "AAAAA-BBBBB-CCCCC-DDDDD", "new_password": "whatever-123",
			"device_id": "ali-phone", "device_name": "Ali",
		})
		last = res.Code
		if i < 5 && res.Code != http.StatusUnauthorized {
			t.Fatalf("try %d: want 401, got %d %s", i+1, res.Code, res.Body.String())
		}
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("6th wrong try must wait: %d", last)
	}
	// Unknown name: the same answer as a wrong code.
	b := newAPI222(t)
	res := doJSON(t, b.ResetPasswordWithCode222, http.MethodPost, "/api/v1/account/recovery/reset", map[string]any{
		"username": "nobody", "recovery_code": "AAAAA-BBBBB-CCCCC-DDDDD", "new_password": "whatever-123", "device_id": "x", "device_name": "X",
	})
	if res.Code != http.StatusUnauthorized || !strings.Contains(res.Body.String(), "BAD_RECOVERY_CODE") {
		t.Fatalf("unknown name: %d %s", res.Code, res.Body.String())
	}
	// Too short new password.
	res = doJSON(t, b.ResetPasswordWithCode222, http.MethodPost, "/api/v1/account/recovery/reset", map[string]any{
		"username": "nobody", "recovery_code": "AAAAA-BBBBB-CCCCC-DDDDD", "new_password": "short", "device_id": "x", "device_name": "X",
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("short password: %d", res.Code)
	}
}

func TestResetWithCodePhoneOfSomeoneElse222(t *testing.T) {
	a := newAPI222(t)
	s := register222(t, a, "sumaira", "old-password-1", "phone-a")
	register222(t, a, "zeeshan", "zee-password-1", "zee-phone")
	res := doJSON(t, a.ResetPasswordWithCode222, http.MethodPost, "/api/v1/account/recovery/reset", map[string]any{
		"username": "sumaira", "recovery_code": s.RecoveryCode, "new_password": "new-password-2", "device_id": "zee-phone", "device_name": "Z",
	})
	if res.Code != http.StatusConflict {
		t.Fatalf("another account's phone id must be refused: %d %s", res.Code, res.Body.String())
	}
	// Nothing changed: the old password still works.
	if c, _ := login222(t, a, "sumaira", "old-password-1", "phone-a"); c != http.StatusOK {
		t.Fatalf("password must be unchanged: %d", c)
	}
}

func TestResetOfflineHashSync222(t *testing.T) {
	a := newAPI222(t)
	s := register222(t, a, "sumaira", "old-password-1", "phone-a")
	salt := make([]byte, passwordSaltLength)
	_, _ = rand.Read(salt)
	hash := derivePasswordKey("offline-new-pass", salt, passwordIterations)
	res := doJSON(t, a.ResetPasswordWithCode222, http.MethodPost, "/api/v1/account/recovery/reset", map[string]any{
		"username": "sumaira", "recovery_code": s.RecoveryCode,
		"new_password_salt": base64.RawURLEncoding.EncodeToString(salt), "new_password_hash": base64.RawURLEncoding.EncodeToString(hash),
		"iterations": passwordIterations, "device_id": "phone-a", "device_name": "Phone A",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("offline sync: %d %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "offline-new-pass") {
		t.Fatal("password must never be echoed")
	}
	if c, _ := login222(t, a, "sumaira", "offline-new-pass", "phone-z"); c != http.StatusOK {
		t.Fatalf("the offline password must work on the server now: %d", c)
	}
	bad := doJSON(t, a.ResetPasswordWithCode222, http.MethodPost, "/api/v1/account/recovery/reset", map[string]any{
		"username": "sumaira", "recovery_code": s.RecoveryCode, "new_password_salt": "abc", "new_password_hash": "def", "iterations": 5,
		"device_id": "phone-a", "device_name": "Phone A",
	})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad hash data: %d", bad.Code)
	}
}

func TestNoCodeRequestToAdmin222(t *testing.T) {
	a := newAPI222(t)
	register222(t, a, "sumaira", "old-password-1", "phone-a")
	_, other := login222(t, a, "sumaira", "old-password-1", "phone-b")
	admin := map[string]string{"X-Admin-Token": "admin-secret"}

	// The exact old failure: username only used to be "invalid recovery request data".
	unknown := doJSON(t, a.CreateAccountRecovery, http.MethodPost, "/api/v1/account/recovery/request", map[string]any{
		"username": "nobody", "no_code": true, "device_id": "phone-a", "device_name": "Phone A", "identity_public_key": testIdentityPublicKey(),
	})
	if unknown.Code != http.StatusNotFound || !strings.Contains(unknown.Body.String(), "NO_ACCOUNT") {
		t.Fatalf("unknown name: %d %s", unknown.Code, unknown.Body.String())
	}
	req := doJSON(t, a.CreateAccountRecovery, http.MethodPost, "/api/v1/account/recovery/request", map[string]any{
		"username": "sumaira", "password": "", "no_code": true, "device_id": "phone-a", "device_name": "Phone A", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	})
	if req.Code != http.StatusAccepted {
		t.Fatalf("no-code request must be saved: %d %s", req.Code, req.Body.String())
	}
	var created struct {
		Request       AccountRecoveryRequest `json:"request"`
		RequestSecret string                 `json:"request_secret"`
		Kind          string                 `json:"kind"`
	}
	_ = json.Unmarshal(req.Body.Bytes(), &created)
	if created.Kind != "no_code" {
		t.Fatalf("kind: %q", created.Kind)
	}
	exp, _ := time.Parse(time.RFC3339Nano, created.Request.ExpiresAt)
	if time.Until(exp) < 365*24*time.Hour {
		t.Fatalf("a no-code request has no time limit: ends %s", created.Request.ExpiresAt)
	}
	list := doJSON(t, a.AdminRecoveryRequests, http.MethodGet, "/api/v1/admin/recovery-requests?limit=50", nil, admin)
	if !strings.Contains(list.Body.String(), `"kind":"no_code"`) || !strings.Contains(list.Body.String(), `"status":"pending"`) {
		t.Fatalf("admin must see the no-code request: %s", list.Body.String())
	}
	ap := doJSON(t, a.AdminApproveRecovery, http.MethodPost, "/api/v1/admin/recovery-requests/approve?id="+created.Request.RequestID, nil, admin)
	if ap.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", ap.Code, ap.Body.String())
	}
	var approval map[string]any
	_ = json.Unmarshal(ap.Body.Bytes(), &approval)
	short, _ := approval["recovery_credential"].(string)
	if !regexp.MustCompile(`^\d{4}-\d{4}-\d{4}$`).MatchString(short) {
		t.Fatalf("no-code requests get a short code to read out: %q", short)
	}
	body := map[string]any{
		"request_id": created.Request.RequestID, "request_secret": created.RequestSecret,
		"recovery_credential": strings.ReplaceAll(short, "-", " "),
		"device_id":           "phone-a", "device_name": "Phone A", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	}
	noPass := doJSON(t, a.CompleteAccountRecovery, http.MethodPost, "/api/v1/account/recovery/complete", body)
	if noPass.Code != http.StatusBadRequest {
		t.Fatalf("a no-code finish needs the new password: %d %s", noPass.Code, noPass.Body.String())
	}
	body["new_password"] = "brand-new-pass-9"
	done := doJSON(t, a.CompleteAccountRecovery, http.MethodPost, "/api/v1/account/recovery/complete", body)
	if done.Code != http.StatusOK {
		t.Fatalf("complete on the SAME (already registered) phone: %d %s", done.Code, done.Body.String())
	}
	var fin struct {
		Token        string `json:"token"`
		RecoveryCode string `json:"recovery_code"`
	}
	_ = json.Unmarshal(done.Body.Bytes(), &fin)
	if fin.Token == "" || !recoveryFormat218.MatchString(fin.RecoveryCode) {
		t.Fatalf("finish must sign in and show a new code: %s", done.Body.String())
	}
	if !signedIn222(a, "phone-b", other.Token) {
		t.Fatal("other phone must stay signed in")
	}
	if c, _ := login222(t, a, "sumaira", "brand-new-pass-9", "phone-q"); c != http.StatusOK {
		t.Fatalf("new password must work: %d", c)
	}
	// One time only.
	if again := doJSON(t, a.CompleteAccountRecovery, http.MethodPost, "/api/v1/account/recovery/complete", body); again.Code == http.StatusOK {
		t.Fatal("the short code must be one-time")
	}
}

func TestOldPasswordRecoveryStillMoves222(t *testing.T) {
	// The old "lost my phone" path (password + new phone) is unchanged: kind move,
	// long credential, and it still signs out the old phone.
	a := newAPI222(t)
	register222(t, a, "hamza", "hamza-password-1", "old-phone")
	req := doJSON(t, a.CreateAccountRecovery, http.MethodPost, "/api/v1/account/recovery/request", map[string]any{
		"username": "hamza", "password": "hamza-password-1", "device_id": "new-phone", "device_name": "New", "identity_public_key": testIdentityPublicKey(),
	})
	if req.Code != http.StatusAccepted || !strings.Contains(req.Body.String(), `"kind":"move"`) {
		t.Fatalf("move request: %d %s", req.Code, req.Body.String())
	}
	var created struct {
		Request AccountRecoveryRequest `json:"request"`
	}
	_ = json.Unmarshal(req.Body.Bytes(), &created)
	ap := doJSON(t, a.AdminApproveRecovery, http.MethodPost, "/api/v1/admin/recovery-requests/approve?id="+created.Request.RequestID, nil, map[string]string{"X-Admin-Token": "admin-secret"})
	var approval map[string]any
	_ = json.Unmarshal(ap.Body.Bytes(), &approval)
	if c, _ := approval["recovery_credential"].(string); len(c) < 40 {
		t.Fatalf("move requests keep the long credential: %q", c)
	}
}
