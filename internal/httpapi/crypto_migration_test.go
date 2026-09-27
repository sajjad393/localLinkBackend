package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestIdentityKeyRotationPreservesHistory(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "keyrotate", "password": "correct-horse-battery", "device_id": "device-a", "device_name": "Phone A", "platform": "android",
	})
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "device-a"}

	initial := doJSON(t, a.PutIdentityKey, http.MethodPut, "/api/v1/device-key", map[string]any{"public_key": testIdentityPublicKey()}, headers)
	if initial.Code != http.StatusOK {
		t.Fatalf("initial key: %d %s", initial.Code, initial.Body.String())
	}

	rotatedKey := "AQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	rotated := doJSON(t, a.RotateIdentityKey, http.MethodPost, "/api/v1/device-key/rotate", map[string]any{"public_key": rotatedKey}, headers)
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rotated.Code, rotated.Body.String())
	}
	var result RotateIdentityKeyResponse
	if err := json.Unmarshal(rotated.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.KeyVersion != 2 || result.PreviousKeyVersion != 1 || result.PublicKey != rotatedKey || result.PreviousFingerprint == "" {
		t.Fatalf("bad rotation result: %+v", result)
	}

	var history []IdentityKeyHistoryRecord
	hist := doJSON(t, a.IdentityKeyHistory, http.MethodGet, "/api/v1/device-key/history", nil, headers)
	if hist.Code != http.StatusOK {
		t.Fatalf("history: %d %s", hist.Code, hist.Body.String())
	}
	if err := json.Unmarshal(hist.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || !history[0].Active || history[0].KeyVersion != 2 || history[1].Active || history[1].KeyVersion != 1 {
		t.Fatalf("unexpected history: %+v", history)
	}

	old := doJSON(t, a.PutIdentityKey, http.MethodPut, "/api/v1/device-key", map[string]any{"public_key": testIdentityPublicKey()}, headers)
	if old.Code != http.StatusConflict {
		t.Fatalf("old key should be rejected: %d %s", old.Code, old.Body.String())
	}
}

func TestAllIdentityKeyHistoryIsAccountScoped(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()
	regA := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{"username": "acctone", "password": "correct-horse-battery", "device_id": "device-a", "device_name": "A"})
	regB := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{"username": "accttwo", "password": "correct-horse-battery", "device_id": "device-b", "device_name": "B"})
	if regA.Code != http.StatusCreated || regB.Code != http.StatusCreated {
		t.Fatalf("register accounts: %d %d", regA.Code, regB.Code)
	}
	var authA, authB AuthResponse
	_ = json.Unmarshal(regA.Body.Bytes(), &authA)
	_ = json.Unmarshal(regB.Body.Bytes(), &authB)
	hA := map[string]string{"Authorization": "Bearer " + authA.Token, "X-Device-ID": "device-a"}
	hB := map[string]string{"Authorization": "Bearer " + authB.Token, "X-Device-ID": "device-b"}
	_ = doJSON(t, a.PutIdentityKey, http.MethodPut, "/api/v1/device-key", map[string]any{"public_key": testIdentityPublicKey()}, hA)
	_ = doJSON(t, a.PutIdentityKey, http.MethodPut, "/api/v1/device-key", map[string]any{"public_key": "AQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}, hB)
	history := doJSON(t, a.AllIdentityKeyHistory, http.MethodGet, "/api/v1/device-keys/history", nil, hA)
	if history.Code != http.StatusOK {
		t.Fatalf("history: %d %s", history.Code, history.Body.String())
	}
	var records []IdentityKeyHistoryRecord
	if err := json.Unmarshal(history.Body.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.DeviceID != "device-a" {
			t.Fatalf("account history leaked device %q", record.DeviceID)
		}
	}
}

func TestAllIdentityKeyHistoryIncludesRevokedAccountDevices(t *testing.T) {
	st, a := newTestAPI(t)
	defer st.Close()
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{"username": "restorekeys", "password": "correct-horse-battery", "device_id": "device-a", "device_name": "A"})
	if reg.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.Code, reg.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	h := map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "device-a"}
	key1 := "AQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if res := doJSON(t, a.PutIdentityKey, http.MethodPut, "/api/v1/device-key", map[string]any{"public_key": key1}, h); res.Code != http.StatusOK {
		t.Fatalf("put key: %d", res.Code)
	}
	key2 := "AgAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if res := doJSON(t, a.RotateIdentityKey, http.MethodPost, "/api/v1/device-key/rotate", map[string]any{"public_key": key2}, h); res.Code != http.StatusOK {
		t.Fatalf("rotate: %d", res.Code)
	}
	if _, err := st.DB().Exec("UPDATE devices SET status='revoked', revoked_at=CURRENT_TIMESTAMP WHERE id='device-a'"); err != nil {
		t.Fatal(err)
	}
	// Add another active device for the same account so an authorized account
	// session still exists while the original historical key is revoked.
	login := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "restorekeys", "password": "correct-horse-battery", "device_id": "device-b", "device_name": "B"})
	if login.Code != http.StatusOK {
		t.Fatalf("login second device: %d %s", login.Code, login.Body.String())
	}
	var authB AuthResponse
	if err := json.Unmarshal(login.Body.Bytes(), &authB); err != nil {
		t.Fatal(err)
	}
	hB := map[string]string{"Authorization": "Bearer " + authB.Token, "X-Device-ID": "device-b"}
	history := doJSON(t, a.AllIdentityKeyHistory, http.MethodGet, "/api/v1/device-keys/history", nil, hB)
	if history.Code != http.StatusOK {
		t.Fatalf("history: %d %s", history.Code, history.Body.String())
	}
	var records []IdentityKeyHistoryRecord
	if err := json.Unmarshal(history.Body.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	foundRevoked := false
	for _, record := range records {
		if record.DeviceID == "device-a" && record.KeyVersion == 1 {
			foundRevoked = true
		}
	}
	if !foundRevoked {
		t.Fatalf("revoked device historical key missing: %+v", records)
	}
}
