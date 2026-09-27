package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"locallink/backend/internal/store"
)

func TestAccountRestoreIsAccountScopedAcrossDevices(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()

	regA := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "restore_a", "password": "correct-horse-battery", "device_id": "a-old", "device_name": "Old phone", "platform": "android",
	})
	regB := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "restore_b", "password": "correct-horse-battery", "device_id": "b-phone", "device_name": "B phone", "platform": "android",
	})
	if regA.Code != http.StatusCreated || regB.Code != http.StatusCreated {
		t.Fatalf("register: %d %d", regA.Code, regB.Code)
	}
	var authA, authB AuthResponse
	if err := json.Unmarshal(regA.Body.Bytes(), &authA); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(regB.Body.Bytes(), &authB); err != nil {
		t.Fatal(err)
	}

	if _, _, err := a.insertMessage("a-old", "b-phone", "history-message", "restore-message-1", nil); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	// Same account, new trusted device. The account restore endpoint must see
	// the historical message even though it targets the old device ID.
	login := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "restore_a", "password": "correct-horse-battery", "device_id": "a-new", "device_name": "New phone", "platform": "android",
	})
	if login.Code != http.StatusOK {
		t.Fatalf("new-device login: %d %s", login.Code, login.Body.String())
	}
	var authNew AuthResponse
	if err := json.Unmarshal(login.Body.Bytes(), &authNew); err != nil {
		t.Fatal(err)
	}
	h := map[string]string{"Authorization": "Bearer " + authNew.Token, "X-Device-ID": "a-new"}

	manifest := doJSON(t, a.GetAccountRestoreManifest, http.MethodGet, "/api/v1/account/restore/manifest", nil, h)
	if manifest.Code != http.StatusOK {
		t.Fatalf("manifest: %d %s", manifest.Code, manifest.Body.String())
	}
	var m AccountRestoreManifest
	if err := json.Unmarshal(manifest.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Account.ID != authA.Account.ID || m.Counts["messages"] != 1 {
		t.Fatalf("unexpected restore manifest: %+v", m)
	}

	messages := doJSON(t, a.GetAccountRestoreData, http.MethodGet, "/api/v1/account/restore/data?scope=messages", nil, h)
	if messages.Code != http.StatusOK {
		t.Fatalf("restore messages: %d %s", messages.Code, messages.Body.String())
	}
	var page AccountRestoreData
	if err := json.Unmarshal(messages.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != "restore-message-1" || page.Messages[0].Body != "history-message" {
		t.Fatalf("unexpected restored messages: %+v", page.Messages)
	}

	// An unrelated account must not see the historical record.
	regC := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "restore_c", "password": "correct-horse-battery", "device_id": "c-phone", "device_name": "C phone", "platform": "android",
	})
	if regC.Code != http.StatusCreated {
		t.Fatalf("register unrelated account: %d %s", regC.Code, regC.Body.String())
	}
	var authC AuthResponse
	if err := json.Unmarshal(regC.Body.Bytes(), &authC); err != nil {
		t.Fatal(err)
	}
	other := map[string]string{"Authorization": "Bearer " + authC.Token, "X-Device-ID": "c-phone"}
	otherPage := doJSON(t, a.GetAccountRestoreData, http.MethodGet, "/api/v1/account/restore/data?scope=messages", nil, other)
	if otherPage.Code != http.StatusOK {
		t.Fatalf("other restore: %d %s", otherPage.Code, otherPage.Body.String())
	}
	var otherData AccountRestoreData
	if err := json.Unmarshal(otherPage.Body.Bytes(), &otherData); err != nil {
		t.Fatal(err)
	}
	if len(otherData.Messages) != 0 {
		t.Fatalf("cross-account message leaked: %+v", otherData.Messages)
	}

}

func TestAccountCryptoBackupLifecycle(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "backupuser", "password": "correct-horse-battery", "device_id": "backup-device", "device_name": "Phone", "platform": "android",
	})
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d", reg.Code)
	}
	var auth AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	h := map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "backup-device"}
	valid := map[string]any{"envelope": "locallink-crypto-backup:v1:test-envelope"}
	put := doJSON(t, a.PutCryptoBackup, http.MethodPut, "/api/v1/account/crypto-backup", valid, h)
	if put.Code != http.StatusOK {
		t.Fatalf("put backup: %d %s", put.Code, put.Body.String())
	}
	get := doJSON(t, a.GetCryptoBackup, http.MethodGet, "/api/v1/account/crypto-backup", nil, h)
	if get.Code != http.StatusOK {
		t.Fatalf("get backup: %d %s", get.Code, get.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["available"] != true || got["envelope"] != valid["envelope"] {
		t.Fatalf("unexpected backup: %#v", got)
	}
	bad := doJSON(t, a.PutCryptoBackup, http.MethodPut, "/api/v1/account/crypto-backup", map[string]any{"envelope": "not-a-local-backup"}, h)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad backup expected 400, got %d", bad.Code)
	}
	remove := doJSON(t, a.DeleteCryptoBackup, http.MethodDelete, "/api/v1/account/crypto-backup", nil, h)
	if remove.Code != http.StatusOK {
		t.Fatalf("delete backup: %d %s", remove.Code, remove.Body.String())
	}
	get2 := doJSON(t, a.GetCryptoBackup, http.MethodGet, "/api/v1/account/crypto-backup", nil, h)
	if get2.Code != http.StatusOK {
		t.Fatalf("get after delete: %d %s", get2.Code, get2.Body.String())
	}
}

func TestRestoreMigrationFreshDatabaseHasCryptoBackupTable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var name string
	if err := st.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='account_crypto_backups'").Scan(&name); err != nil {
		t.Fatalf("crypto backup table missing: %v", err)
	}
	if name != "account_crypto_backups" {
		t.Fatalf("unexpected table name %q", name)
	}
}
