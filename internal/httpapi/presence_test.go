package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"locallink/backend/internal/store"
)

func TestPresenceStatus(t *testing.T) {
	tests := []struct {
		name string
		p    DevicePresence
		want string
	}{
		{"yellow", DevicePresence{ServerConnected: true}, "yellow"},
		{"orange", DevicePresence{ServerConnected: true, WiFiDirectConnected: true}, "orange"},
		{"red", DevicePresence{}, "red"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := presenceStatus(tt.p); got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestPresenceStale(t *testing.T) {
	if !presenceStale(DevicePresence{UpdatedAt: time.Now().UTC().Add(-presenceTTL - time.Second).Format(time.RFC3339Nano)}) {
		t.Fatal("expected old presence to be stale")
	}
	if presenceStale(DevicePresence{UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}) {
		t.Fatal("fresh presence should not be stale")
	}
}

func TestLegacyPeerKeysAreDisabled(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	res := doJSON(t, a.PeerKeys, "GET", "/api/v1/peer-keys", nil, nil)
	if res.Code != 410 {
		t.Fatalf("expected 410, got %d %s", res.Code, res.Body.String())
	}
	res = doJSON(t, a.PeerKey, "GET", "/api/v1/peer-key?peer_id=b", nil, nil)
	if res.Code != 410 {
		t.Fatalf("expected 410, got %d %s", res.Code, res.Body.String())
	}
}

func TestListDevicesMarksStalePresenceUnknown(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	res := doJSON(t, a.RegisterDevice, "POST", "/api/v1/devices", map[string]any{"id": "a", "name": "a", "platform": "android"}, nil)
	if res.Code != 201 {
		t.Fatalf("register: %d %s", res.Code, res.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	res = doJSON(t, a.RegisterDevice, "POST", "/api/v1/devices", map[string]any{"id": "b", "name": "b", "platform": "android"}, nil)
	if res.Code != 201 {
		t.Fatalf("register b: %d %s", res.Code, res.Body.String())
	}
	a.presenceMu.Lock()
	a.presence["b"] = DevicePresence{
		ServerConnected: true,
		UpdatedAt:       time.Now().UTC().Add(-presenceTTL - time.Second).Format(time.RFC3339Nano),
	}
	a.presenceMu.Unlock()
	list := doJSON(t, a.ListDevices, "GET", "/api/v1/devices", nil, map[string]string{
		"Authorization": "Bearer " + out.Token,
		"X-Device-ID":   "a",
	})
	if list.Code != 200 {
		t.Fatalf("list devices: %d %s", list.Code, list.Body.String())
	}
	var page struct {
		Devices []Device `json:"devices"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	devices := page.Devices
	for _, d := range devices {
		if d.ID == "b" {
			if d.NetworkStatus != "unknown" || d.ServerConnected || d.WiFiDirectConnected {
				t.Fatalf("stale device was not reset: %+v", d)
			}
			return
		}
	}
	t.Fatal("device b not returned")
}

func TestIdentityKeysAreAuthenticatedAndValidated(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/locallink.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	register := func(id string) string {
		res := doJSON(t, a.RegisterDevice, "POST", "/api/v1/devices", map[string]any{"id": id, "name": id, "platform": "android"}, nil)
		if res.Code != 201 {
			t.Fatalf("register %s: %d %s", id, res.Code, res.Body.String())
		}
		var out struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Token
	}
	ta := register("a")
	tb := register("b")
	bad := doJSON(t, a.PutIdentityKey, "PUT", "/api/v1/device-key", map[string]any{"public_key": "bad"}, map[string]string{"Authorization": "Bearer " + ta, "X-Device-ID": "a"})
	if bad.Code != 400 {
		t.Fatalf("expected invalid key 400, got %d", bad.Code)
	}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	good := doJSON(t, a.PutIdentityKey, "PUT", "/api/v1/device-key", map[string]any{"public_key": key}, map[string]string{"Authorization": "Bearer " + ta, "X-Device-ID": "a"})
	if good.Code != 200 {
		t.Fatalf("put key failed: %d %s", good.Code, good.Body.String())
	}
	peerKey := "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
	goodPeer := doJSON(t, a.PutIdentityKey, "PUT", "/api/v1/device-key", map[string]any{"public_key": peerKey}, map[string]string{"Authorization": "Bearer " + tb, "X-Device-ID": "b"})
	if goodPeer.Code != 200 {
		t.Fatalf("put peer key failed: %d %s", goodPeer.Code, goodPeer.Body.String())
	}
	keys := doJSON(t, a.IdentityKeys, "GET", "/api/v1/device-keys", nil, map[string]string{"Authorization": "Bearer " + ta, "X-Device-ID": "a"})
	if keys.Code != 200 || !strings.Contains(keys.Body.String(), `"peer_id":"b"`) {
		t.Fatalf("identity keys failed: %d %s", keys.Code, keys.Body.String())
	}
}
