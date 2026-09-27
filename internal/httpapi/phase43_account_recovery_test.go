package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func phase43RegisterAccount(t *testing.T, a *API, username, deviceID string) AuthResponse {
	t.Helper()
	res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": username, "password": "correct-horse-battery", "device_id": deviceID, "device_name": deviceID, "platform": "android",
	}, nil)
	if res.Code != http.StatusCreated {
		t.Fatalf("register %s: %d %s", username, res.Code, res.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(res.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	return auth
}

func phase43Headers(token, deviceID string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token, "X-Device-ID": deviceID}
}

func phase43RecoveryRequest(t *testing.T, a *API, username, targetID string) (AccountRecoveryRequest, string) {
	t.Helper()
	res := doJSON(t, a.CreateAccountRecovery, http.MethodPost, "/api/v1/account/recovery/request", map[string]any{
		"username": username, "password": "correct-horse-battery", "device_id": targetID, "device_name": targetID, "platform": "android", "identity_public_key": testIdentityPublicKey(),
	}, nil)
	if res.Code != http.StatusAccepted {
		t.Fatalf("create recovery request: %d %s", res.Code, res.Body.String())
	}
	var out struct {
		Request       AccountRecoveryRequest `json:"request"`
		RequestSecret string                 `json:"request_secret"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Request, out.RequestSecret
}

func phase43CountByID(t *testing.T, db *sql.DB, table, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id=?", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPhase43RecoveryCredentialBoundaries(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()
	t.Setenv("LOCALLINK_ADMIN_TOKEN", "phase43-admin")

	auth := phase43RegisterAccount(t, a, "phase43recover", "old-phone")
	req, secret := phase43RecoveryRequest(t, a, "phase43recover", "new-phone")

	// Wrong admin credentials must not change the request.
	unauthorizedApproval := doJSON(t, a.AdminApproveRecovery, http.MethodPost, "/api/v1/admin/recovery-requests/approve?id="+req.RequestID, nil, map[string]string{"X-Admin-Token": "wrong"})
	if unauthorizedApproval.Code != http.StatusUnauthorized {
		t.Fatalf("wrong admin token expected 401, got %d", unauthorizedApproval.Code)
	}

	approval := doJSON(t, a.AdminApproveRecovery, http.MethodPost, "/api/v1/admin/recovery-requests/approve?id="+req.RequestID, nil, map[string]string{"X-Admin-Token": "phase43-admin"})
	if approval.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", approval.Code, approval.Body.String())
	}
	var approved map[string]any
	if err := json.Unmarshal(approval.Body.Bytes(), &approved); err != nil {
		t.Fatal(err)
	}
	credential, _ := approved["recovery_credential"].(string)
	if credential == "" {
		t.Fatal("missing recovery credential")
	}

	mismatchedKey := doJSON(t, a.CompleteAccountRecovery, http.MethodPost, "/api/v1/account/recovery/complete", map[string]any{
		"request_id": req.RequestID, "request_secret": secret, "recovery_credential": credential, "device_id": "new-phone", "device_name": "New", "platform": "android", "identity_public_key": "AQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}, nil)
	if mismatchedKey.Code != http.StatusConflict {
		t.Fatalf("mismatched identity key expected 409, got %d %s", mismatchedKey.Code, mismatchedKey.Body.String())
	}

	badCredential := doJSON(t, a.CompleteAccountRecovery, http.MethodPost, "/api/v1/account/recovery/complete", map[string]any{
		"request_id": req.RequestID, "request_secret": secret, "recovery_credential": "wrong", "device_id": "new-phone", "device_name": "New", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	}, nil)
	if badCredential.Code != http.StatusUnauthorized {
		t.Fatalf("bad credential expected 401, got %d %s", badCredential.Code, badCredential.Body.String())
	}
	if phase43CountByID(t, st.DB(), "devices", "new-phone") != 0 {
		t.Fatal("invalid recovery credential created a target device")
	}

	complete := doJSON(t, a.CompleteAccountRecovery, http.MethodPost, "/api/v1/account/recovery/complete", map[string]any{
		"request_id": req.RequestID, "request_secret": secret, "recovery_credential": credential, "device_id": "new-phone", "device_name": "New", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	}, nil)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", complete.Code, complete.Body.String())
	}

	var oldStatus, oldToken string
	if err := st.DB().QueryRow("SELECT status FROM devices WHERE id='old-phone'").Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "revoked" {
		t.Fatalf("old device status: %q", oldStatus)
	}
	if err := st.DB().QueryRow("SELECT COALESCE(token_hash,'') FROM devices WHERE id='old-phone'").Scan(&oldToken); err != nil {
		t.Fatal(err)
	}
	if oldToken != "" {
		t.Fatal("revoked device token hash was retained")
	}
	if got := doJSON(t, a.AuthMe, http.MethodGet, "/api/v1/auth/me", nil, phase43Headers(auth.Token, "old-phone")).Code; got != http.StatusUnauthorized {
		t.Fatalf("old session still valid: %d", got)
	}

	var keyHistoryCount int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM device_identity_keys WHERE device_id='new-phone' AND key_version=1 AND public_key=?`, testIdentityPublicKey()).Scan(&keyHistoryCount); err != nil {
		t.Fatal(err)
	}
	if keyHistoryCount != 1 {
		t.Fatalf("recovered device initial identity key history missing: %d", keyHistoryCount)
	}

	// The credential is single-use and the request is completed.
	replay := doJSON(t, a.CompleteAccountRecovery, http.MethodPost, "/api/v1/account/recovery/complete", map[string]any{
		"request_id": req.RequestID, "request_secret": secret, "recovery_credential": credential, "device_id": "new-phone-2", "device_name": "New2", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	}, nil)
	if replay.Code != http.StatusConflict {
		t.Fatalf("replay expected 409, got %d", replay.Code)
	}
}

func TestPhase43RecoveryExpiryAndReplacementSemantics(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	phase43RegisterAccount(t, a, "phase43expiry", "phone")
	first, _ := phase43RecoveryRequest(t, a, "phase43expiry", "target-one")
	second, secondSecret := phase43RecoveryRequest(t, a, "phase43expiry", "target-two")

	var firstStatus string
	if err := st.DB().QueryRow("SELECT status FROM account_recovery_requests WHERE id=?", first.RequestID).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "expired" {
		t.Fatalf("superseded request should become expired, got %q", firstStatus)
	}

	// An approved request whose request TTL has elapsed must be treated as
	// expired even when its short-lived recovery credential has not elapsed yet.
	expiredRequestAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	approvedAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	recoveryExpiry := time.Now().UTC().Add(9 * time.Minute).Format(time.RFC3339Nano)
	if _, err := st.DB().Exec("UPDATE account_recovery_requests SET status='approved', expires_at=?, approved_at=?, recovery_expires_at=?, recovery_credential_hash=? WHERE id=?", expiredRequestAt, approvedAt, recoveryExpiry, tokenHash("credential"), second.RequestID); err != nil {
		t.Fatal(err)
	}
	status := doJSON(t, a.AccountRecoveryStatus, http.MethodPost, "/api/v1/account/recovery/status", map[string]any{"request_id": second.RequestID, "request_secret": secondSecret}, nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"status":"expired"`) {
		t.Fatalf("expired approved request status: %d %s", status.Code, status.Body.String())
	}

	var storedStatus string
	if err := st.DB().QueryRow("SELECT status FROM account_recovery_requests WHERE id=?", second.RequestID).Scan(&storedStatus); err != nil {
		t.Fatal(err)
	}
	if storedStatus != "expired" {
		t.Fatalf("poll should persist expired status, got %q", storedStatus)
	}
}

func TestPhase43ProductionCleanupAllRecoveryStates(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	auth := phase43RegisterAccount(t, a, "phase43cleanup", "cleanup-device")
	now := time.Now().UTC()
	old := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	veryOld := now.Add(-200 * 24 * time.Hour).Format(time.RFC3339Nano)

	states := []string{"pending", "approved", "rejected", "expired"}
	for i, state := range states {
		id := "cleanup-recovery-" + itoa(i)
		if _, err := st.DB().Exec(`INSERT INTO account_recovery_requests(id,user_id,target_device_id,target_device_name,target_platform,target_identity_public_key,request_secret_hash,status,created_at,expires_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, auth.Account.ID, "target-"+itoa(i), "Target", "android", testIdentityPublicKey(), tokenHash(id), state, veryOld, old, old); err != nil {
			t.Fatal(err)
		}
	}
	completedOld := "cleanup-recovery-completed-old"
	if _, err := st.DB().Exec(`INSERT INTO account_recovery_requests(id,user_id,target_device_id,target_device_name,target_platform,target_identity_public_key,request_secret_hash,status,created_at,expires_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, completedOld, auth.Account.ID, "completed-target", "Target", "android", testIdentityPublicKey(), tokenHash(completedOld), "completed", veryOld, old, veryOld); err != nil {
		t.Fatal(err)
	}
	if err := a.CleanupProductionArtifacts(); err != nil {
		t.Fatal(err)
	}

	var remaining int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM account_recovery_requests WHERE id LIKE 'cleanup-recovery-%'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("stale recovery rows remain after cleanup: %d", remaining)
	}
}

func TestPhase43TransferAndSessionCleanup(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	auth := phase43RegisterAccount(t, a, "phase43transfercleanup", "source")
	start := doJSON(t, a.StartAccountTransfer, http.MethodPost, "/api/v1/account/transfer/start", nil, phase43Headers(auth.Token, "source"))
	if start.Code != http.StatusCreated {
		t.Fatalf("start transfer: %d %s", start.Code, start.Body.String())
	}
	var created AccountTransferStart
	if err := json.Unmarshal(start.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	if _, err := st.DB().Exec("UPDATE account_transfer_sessions SET expires_at=? WHERE id=?", old, created.TransferID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec("UPDATE auth_sessions SET expires_at=? WHERE device_id='source'", old); err != nil {
		t.Fatal(err)
	}
	if err := a.CleanupProductionArtifacts(); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := st.DB().QueryRow("SELECT COUNT(*) FROM account_transfer_sessions WHERE id=?", created.TransferID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("expired transfer session was not cleaned")
	}
	if err := st.DB().QueryRow("SELECT COUNT(*) FROM auth_sessions WHERE device_id='source'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("expired authentication session was not cleaned")
	}
}

func TestPhase43RestorePaginationUsesStableCreatedAtAndIDCursor(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	auth := phase43RegisterAccount(t, a, "phase43restore", "restore-device")
	other := phase43RegisterAccount(t, a, "phase43other", "other-device")

	// Force identical created_at values so the secondary ID cursor component is
	// required to make pagination deterministic.
	ids := []string{"restore-1", "restore-2", "restore-3"}
	for _, id := range ids {
		if _, _, err := a.insertMessage("restore-device", "restore-device", "body-"+id, id, nil); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	stamp := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	for _, id := range ids {
		if _, err := st.DB().Exec("UPDATE messages SET created_at=? WHERE id=?", stamp, id); err != nil {
			t.Fatal(err)
		}
	}
	// Add an unrelated-account record with the same timestamp.
	if _, err := st.DB().Exec(`INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status,server_seq) VALUES(?,?,?,?,?,?,?)`, "other-msg", other.Device.ID, other.Device.ID, "other", stamp, "sent", 9999); err != nil {
		t.Fatal(err)
	}

	h := phase43Headers(auth.Token, "restore-device")
	first := doJSON(t, a.GetAccountRestoreData, http.MethodGet, "/api/v1/account/restore/data?scope=messages&limit=2", nil, h)
	if first.Code != http.StatusOK {
		t.Fatalf("first restore page: %d %s", first.Code, first.Body.String())
	}
	var page1 AccountRestoreData
	if err := json.Unmarshal(first.Body.Bytes(), &page1); err != nil {
		t.Fatal(err)
	}
	if len(page1.Messages) != 2 || !page1.HasMore || page1.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", page1)
	}

	second := doJSON(t, a.GetAccountRestoreData, http.MethodGet, "/api/v1/account/restore/data?scope=messages&limit=2&cursor="+page1.NextCursor, nil, h)
	if second.Code != http.StatusOK {
		t.Fatalf("second restore page: %d %s", second.Code, second.Body.String())
	}
	var page2 AccountRestoreData
	if err := json.Unmarshal(second.Body.Bytes(), &page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.Messages) != 1 || page2.HasMore || page2.NextCursor == "" {
		t.Fatalf("unexpected second page: %+v", page2)
	}
	seen := map[string]bool{}
	for _, m := range append(page1.Messages, page2.Messages...) {
		seen[m.ID] = true
		if m.ID == "other-msg" {
			t.Fatal("cross-account message leaked through restore pagination")
		}
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 unique restored messages, got %d", len(seen))
	}
}

func TestPhase43CryptoBackupIsAccountScopedAndOverwrite(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	one := phase43RegisterAccount(t, a, "phase43backupone", "one")
	two := phase43RegisterAccount(t, a, "phase43backuptwo", "two")

	put := doJSON(t, a.PutCryptoBackup, http.MethodPut, "/api/v1/account/crypto-backup", map[string]any{"envelope": "locallink-crypto-backup:v1:one"}, phase43Headers(one.Token, "one"))
	if put.Code != http.StatusOK {
		t.Fatalf("put one: %d %s", put.Code, put.Body.String())
	}
	getTwo := doJSON(t, a.GetCryptoBackup, http.MethodGet, "/api/v1/account/crypto-backup", nil, phase43Headers(two.Token, "two"))
	if getTwo.Code != http.StatusOK || !strings.Contains(getTwo.Body.String(), `"available":false`) {
		t.Fatalf("account isolation failed: %d %s", getTwo.Code, getTwo.Body.String())
	}
	putNew := doJSON(t, a.PutCryptoBackup, http.MethodPut, "/api/v1/account/crypto-backup", map[string]any{"envelope": "locallink-crypto-backup:v1:new"}, phase43Headers(one.Token, "one"))
	if putNew.Code != http.StatusOK {
		t.Fatalf("overwrite: %d %s", putNew.Code, putNew.Body.String())
	}
	getOne := doJSON(t, a.GetCryptoBackup, http.MethodGet, "/api/v1/account/crypto-backup", nil, phase43Headers(one.Token, "one"))
	if getOne.Code != http.StatusOK || !strings.Contains(getOne.Body.String(), "locallink-crypto-backup:v1:new") {
		t.Fatalf("overwrite not visible: %d %s", getOne.Code, getOne.Body.String())
	}
}

func TestPhase43TransferInitializesIdentityKeyHistory(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	auth := phase43RegisterAccount(t, a, "phase43transferhistory", "source-device")
	start := doJSON(t, a.StartAccountTransfer, http.MethodPost, "/api/v1/account/transfer/start", nil, phase43Headers(auth.Token, "source-device"))
	if start.Code != http.StatusCreated {
		t.Fatalf("start transfer: %d %s", start.Code, start.Body.String())
	}
	var created AccountTransferStart
	if err := json.Unmarshal(start.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	complete := doJSON(t, a.CompleteAccountTransfer, http.MethodPost, "/api/v1/account/transfer/complete", map[string]any{
		"transfer_id": created.TransferID, "secret": extractTransferSecret(t, created.Payload), "device_id": "target-device", "device_name": "Target", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	}, nil)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete transfer: %d %s", complete.Code, complete.Body.String())
	}
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM device_identity_keys WHERE device_id='target-device' AND key_version=1 AND public_key=?`, testIdentityPublicKey()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("transferred device initial identity key history missing: %d", n)
	}
}

func TestPhase43ClientFileIDValidation(t *testing.T) {
	valid := []string{"ab", "A1_device-01", "file.v1-2"}
	for _, id := range valid {
		if !validClientFileID(id) {
			t.Fatalf("expected valid client file id: %q", id)
		}
	}
	invalid := []string{"", "a", "../escape", "..", "/tmp/file", "\\tmp\\file", ".hidden", "a/b", "a\\b"}
	for _, id := range invalid {
		if validClientFileID(id) {
			t.Fatalf("expected invalid client file id: %q", id)
		}
	}
}
