package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAccountTransferCreateCompleteAndRevokeSource(t *testing.T) {
	_, a := newTestAPI(t)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "transferuser", "password": "correct-horse-battery", "device_id": "old-phone", "device_name": "Old Phone", "platform": "android",
	})
	var auth AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}

	start := doJSON(t, a.StartAccountTransfer, http.MethodPost, "/api/v1/account/transfer/start", nil, map[string]string{
		"Authorization": "Bearer " + auth.Token, "X-Device-ID": "old-phone",
	})
	if start.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", start.Code, start.Body.String())
	}
	var created AccountTransferStart
	if err := json.Unmarshal(start.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.TransferID == "" || created.Payload == "" || created.ExpiresAt == "" {
		t.Fatalf("invalid start response: %+v", created)
	}

	complete := doJSON(t, a.CompleteAccountTransfer, http.MethodPost, "/api/v1/account/transfer/complete", map[string]any{
		"transfer_id":          created.TransferID,
		"secret":               extractTransferSecret(t, created.Payload),
		"device_id":            "new-phone",
		"device_name":          "New Phone",
		"platform":             "android",
		"identity_public_key":  testIdentityPublicKey(),
		"revoke_source_device": true,
	})
	if complete.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", complete.Code, complete.Body.String())
	}
	var result AccountTransferResponse
	if err := json.Unmarshal(complete.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Token == "" || result.Device.ID != "new-phone" || !result.SourceDeviceRevoked {
		t.Fatalf("invalid complete: %+v", result)
	}

	old := doJSON(t, a.AuthMe, http.MethodGet, "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "old-phone"})
	if old.Code != http.StatusUnauthorized {
		t.Fatalf("old device remained authorized: %d %s", old.Code, old.Body.String())
	}
	replay := doJSON(t, a.CompleteAccountTransfer, http.MethodPost, "/api/v1/account/transfer/complete", map[string]any{
		"transfer_id": created.TransferID, "secret": extractTransferSecret(t, created.Payload), "device_id": "new-phone-2", "device_name": "Other", "platform": "android", "identity_public_key": testIdentityPublicKey(),
	})
	if replay.Code != http.StatusGone {
		t.Fatalf("replay should be gone: %d %s", replay.Code, replay.Body.String())
	}
}

func TestAccountTransferInvalidSecretDoesNotConsume(t *testing.T) {
	_, a := newTestAPI(t)
	reg := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "transferbad", "password": "correct-horse-battery", "device_id": "old-phone", "device_name": "Old Phone", "platform": "android",
	})
	var auth AuthResponse
	if err := json.Unmarshal(reg.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	start := doJSON(t, a.StartAccountTransfer, http.MethodPost, "/api/v1/account/transfer/start", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": "old-phone"})
	var created AccountTransferStart
	_ = json.Unmarshal(start.Body.Bytes(), &created)
	bad := doJSON(t, a.CompleteAccountTransfer, http.MethodPost, "/api/v1/account/transfer/complete", map[string]any{"transfer_id": created.TransferID, "secret": "wrong", "device_id": "new-phone", "device_name": "New", "platform": "android", "identity_public_key": testIdentityPublicKey()})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad secret: %d %s", bad.Code, bad.Body.String())
	}
	good := doJSON(t, a.CompleteAccountTransfer, http.MethodPost, "/api/v1/account/transfer/complete", map[string]any{"transfer_id": created.TransferID, "secret": extractTransferSecret(t, created.Payload), "device_id": "new-phone", "device_name": "New", "platform": "android", "identity_public_key": testIdentityPublicKey()})
	if good.Code != http.StatusOK {
		t.Fatalf("good secret after bad attempt: %d %s", good.Code, good.Body.String())
	}
}

func extractTransferSecret(t *testing.T, payload string) string {
	const prefix = "locallink://account-transfer/"
	if len(payload) <= len(prefix) || payload[:len(prefix)] != prefix {
		t.Fatalf("unexpected transfer payload: %s", payload)
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload[len(prefix):])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m["secret"]
}

func testIdentityPublicKey() string {
	return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}
