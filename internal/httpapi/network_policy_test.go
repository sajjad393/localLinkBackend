package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestNetworkPolicyIsLocalOnlyAndWifiOnly(t *testing.T) {
	_, api := newTestAPI(t)
	registered := doJSON(t, api.RegisterAccount, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "policy-test", "password": "a-long-test-password", "device_id": "device-policy", "device_name": "Policy test", "platform": "android",
	})
	if registered.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", registered.Code, registered.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(registered.Body.Bytes(), &auth); err != nil {
		t.Fatal(err)
	}
	got := doJSON(t, api.DeviceGetNetworkPolicy, http.MethodGet, "/api/v1/device/network-policy", nil, map[string]string{"Authorization": "Bearer " + auth.Token, "X-Device-ID": auth.Device.ID})
	if got.Code != http.StatusOK {
		t.Fatalf("get policy: %d %s", got.Code, got.Body.String())
	}
	var policy effectiveNetworkPolicy
	if err := json.Unmarshal(got.Body.Bytes(), &policy); err != nil {
		t.Fatal(err)
	}
	if !policy.LocalOnly {
		t.Fatalf("expected local_only=true: %+v", policy)
	}
}
