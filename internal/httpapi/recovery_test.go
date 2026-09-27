package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"locallink/backend/internal/store"
)

func TestAccountRecoveryRequestApprovalAndCompletion(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t.Setenv("LOCALLINK_ADMIN_TOKEN", "admin-secret")
	a := New(st)

	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "recoveruser", "password": "correct-horse-battery", "device_id": "old-phone", "device_name": "Lost Phone", "platform": "android",
	})
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}

	request := doJSON(t, a.CreateAccountRecovery, http.MethodPost, "/api/v1/account/recovery/request", map[string]any{
		"username": "recoveruser", "password": "correct-horse-battery", "device_id": "new-phone", "device_name": "Replacement Phone", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	})
	if request.Code != http.StatusAccepted {
		t.Fatalf("request: %d %s", request.Code, request.Body.String())
	}
	var created struct {
		Request       AccountRecoveryRequest `json:"request"`
		RequestSecret string                 `json:"request_secret"`
	}
	if err := json.Unmarshal(request.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Request.RequestID == "" || created.RequestSecret == "" || created.Request.Status != "pending" {
		t.Fatalf("bad request response: %+v", created)
	}

	pending := doJSON(t, a.AccountRecoveryStatus, http.MethodPost, "/api/v1/account/recovery/status", map[string]any{"request_id": created.Request.RequestID, "request_secret": created.RequestSecret}, nil)
	if pending.Code != http.StatusOK || !containsJSON(pending.Body.String(), `"status":"pending"`) {
		t.Fatalf("pending: %d %s", pending.Code, pending.Body.String())
	}

	oldCode := doJSON(t, a.AdminApproveRecovery, http.MethodPost, "/api/v1/admin/recovery-requests/approve?id="+created.Request.RequestID, nil, map[string]string{"X-Admin-Token": "admin-secret"})
	if oldCode.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", oldCode.Code, oldCode.Body.String())
	}
	var approval map[string]any
	if err := json.Unmarshal(oldCode.Body.Bytes(), &approval); err != nil {
		t.Fatal(err)
	}
	credential, _ := approval["recovery_credential"].(string)
	if credential == "" {
		t.Fatalf("approval did not return a recovery credential")
	}

	approved := doJSON(t, a.AccountRecoveryStatus, http.MethodPost, "/api/v1/account/recovery/status", map[string]any{"request_id": created.Request.RequestID, "request_secret": created.RequestSecret}, nil)
	if approved.Code != http.StatusOK || !containsJSON(approved.Body.String(), `"status":"approved"`) || containsJSON(approved.Body.String(), credential) {
		t.Fatalf("approved status leaked credential: %d %s", approved.Code, approved.Body.String())
	}

	complete := doJSON(t, a.CompleteAccountRecovery, http.MethodPost, "/api/v1/account/recovery/complete", map[string]any{
		"request_id": created.Request.RequestID, "request_secret": created.RequestSecret, "recovery_credential": credential,
		"device_id": "new-phone", "device_name": "Replacement Phone", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	})
	if complete.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", complete.Code, complete.Body.String())
	}
	var recovered AccountTransferResponse
	if err := json.Unmarshal(complete.Body.Bytes(), &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Token == "" || recovered.Device.ID != "new-phone" || !recovered.SourceDeviceRevoked {
		t.Fatalf("bad recovered response: %+v", recovered)
	}

	old := doJSON(t, a.AuthMe, http.MethodGet, "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "old-phone"})
	if old.Code != http.StatusUnauthorized {
		t.Fatalf("old device remained authorized: %d %s", old.Code, old.Body.String())
	}

	replay := doJSON(t, a.CompleteAccountRecovery, http.MethodPost, "/api/v1/account/recovery/complete", map[string]any{
		"request_id": created.Request.RequestID, "request_secret": created.RequestSecret, "recovery_credential": credential,
		"device_id": "new-phone-2", "device_name": "Another", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	})
	if replay.Code != http.StatusConflict {
		t.Fatalf("replay should be rejected, got %d %s", replay.Code, replay.Body.String())
	}
}

func TestAdminCanReissueRecoveryCredential(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t.Setenv("LOCALLINK_ADMIN_TOKEN", "admin-secret")
	a := New(st)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "reissueuser", "password": "correct-horse-battery", "device_id": "old", "device_name": "Old", "platform": "android",
	})
	var auth AuthResponse
	_ = json.Unmarshal(reg.Body.Bytes(), &auth)
	req := doJSON(t, a.CreateAccountRecovery, http.MethodPost, "/api/v1/account/recovery/request", map[string]any{
		"username": "reissueuser", "password": "correct-horse-battery", "device_id": "new", "device_name": "New", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	})
	var created struct {
		Request AccountRecoveryRequest `json:"request"`
	}
	_ = json.Unmarshal(req.Body.Bytes(), &created)
	approved := doJSON(t, a.AdminApproveRecovery, http.MethodPost, "/api/v1/admin/recovery-requests/approve?id="+created.Request.RequestID, nil, map[string]string{"X-Admin-Token": "admin-secret"})
	if approved.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", approved.Code, approved.Body.String())
	}
	first := map[string]any{}
	_ = json.Unmarshal(approved.Body.Bytes(), &first)
	firstCredential := first["recovery_credential"].(string)
	reissued := doJSON(t, a.AdminReissueRecovery, http.MethodPost, "/api/v1/admin/recovery-requests/reissue?id="+created.Request.RequestID, nil, map[string]string{"X-Admin-Token": "admin-secret"})
	if reissued.Code != http.StatusOK {
		t.Fatalf("reissue: %d %s", reissued.Code, reissued.Body.String())
	}
	second := map[string]any{}
	_ = json.Unmarshal(reissued.Body.Bytes(), &second)
	secondCredential := second["recovery_credential"].(string)
	if secondCredential == "" || secondCredential == firstCredential {
		t.Fatal("reissue did not rotate credential")
	}
}

func containsJSON(body, value string) bool { return strings.Contains(body, value) }

func TestRecoveryCodeLifecycleAndRecoveryRequest(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "codeuser", "password": "correct-horse-battery", "device_id": "phone-a", "device_name": "Phone A", "platform": "android",
	})
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	if auth.RecoveryCode == "" {
		t.Fatal("registration did not return recovery code")
	}
	status := doJSON(t, a.RecoveryCodeStatus, http.MethodGet, "/api/v1/account/recovery-code/status", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "phone-a"})
	if status.Code != http.StatusOK || !containsJSON(status.Body.String(), `"configured":true`) {
		t.Fatalf("status: %d %s", status.Code, status.Body.String())
	}
	newCode := doJSON(t, a.RotateRecoveryCode, http.MethodPost, "/api/v1/account/recovery-code/rotate", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "phone-a"})
	if newCode.Code != http.StatusOK || containsJSON(newCode.Body.String(), auth.RecoveryCode) {
		t.Fatalf("rotate: %d %s", newCode.Code, newCode.Body.String())
	}
	var rotated map[string]any
	_ = json.Unmarshal(newCode.Body.Bytes(), &rotated)
	code, _ := rotated["recovery_code"].(string)
	if code == "" {
		t.Fatal("missing rotated code")
	}
	request := doJSON(t, a.CreateAccountRecovery, http.MethodPost, "/api/v1/account/recovery/request", map[string]any{
		"username": "codeuser", "recovery_code": code, "device_id": "phone-b", "device_name": "Phone B", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	})
	if request.Code != http.StatusAccepted {
		t.Fatalf("code recovery request: %d %s", request.Code, request.Body.String())
	}
}

func TestRecoveryCodeNormalizationAndDisable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "normalizeuser", "password": "correct-horse-battery", "device_id": "phone-a", "device_name": "Phone A", "platform": "android",
	})
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	status := doJSON(t, a.RecoveryCodeStatus, http.MethodGet, "/api/v1/account/recovery-code/status", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "phone-a"})
	if status.Code != http.StatusOK || !containsJSON(status.Body.String(), `"configured":true`) {
		t.Fatalf("status: %d %s", status.Code, status.Body.String())
	}
	rotated := doJSON(t, a.RotateRecoveryCode, http.MethodPost, "/api/v1/account/recovery-code/rotate", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "phone-a"})
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rotated.Code, rotated.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rotated.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	code, _ := payload["recovery_code"].(string)
	if code == "" {
		t.Fatal("missing recovery code")
	}
	variant := strings.ReplaceAll(strings.ToLower(code), "-", "  ")
	request := doJSON(t, a.CreateAccountRecovery, http.MethodPost, "/api/v1/account/recovery/request", map[string]any{
		"username": "normalizeuser", "recovery_code": variant, "device_id": "phone-b", "device_name": "Phone B", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	})
	if request.Code != http.StatusAccepted {
		t.Fatalf("normalized recovery code was rejected: %d %s", request.Code, request.Body.String())
	}
	disable := doJSON(t, a.DisableRecoveryCode, http.MethodPost, "/api/v1/account/recovery-code/disable", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "phone-a"})
	if disable.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", disable.Code, disable.Body.String())
	}
	status2 := doJSON(t, a.RecoveryCodeStatus, http.MethodGet, "/api/v1/account/recovery-code/status", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "phone-a"})
	if status2.Code != http.StatusOK || !containsJSON(status2.Body.String(), `"configured":false`) {
		t.Fatalf("disabled status: %d %s", status2.Code, status2.Body.String())
	}
	fail := doJSON(t, a.CreateAccountRecovery, http.MethodPost, "/api/v1/account/recovery/request", map[string]any{
		"username": "normalizeuser", "recovery_code": code, "device_id": "phone-c", "device_name": "Phone C", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	})
	if fail.Code != http.StatusUnauthorized {
		t.Fatalf("disabled code should fail: %d %s", fail.Code, fail.Body.String())
	}
}
