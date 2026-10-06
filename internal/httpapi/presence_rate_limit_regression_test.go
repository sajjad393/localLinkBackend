package httpapi

import "testing"

func TestSetPresenceReportsUnchangedState(t *testing.T) {
	st, err := newTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)

	// Seed the device row required by last_seen_at update.
	_, err = st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,status) VALUES(?,?,?,?,?,'active')`, "device-a", "A", "android", "2026-09-30T00:00:00Z", "2026-09-30T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	p := DevicePresence{ServerConnected: true, WiFiDirectConnected: false, LanAvailable: true, MeshAvailable: true}
	if _, changed := a.setPresence("device-a", true, false, p); !changed {
		t.Fatal("first presence update should be changed")
	}
	if _, changed := a.setPresence("device-a", true, false, p); changed {
		t.Fatal("identical presence state should not be broadcast as changed")
	}
}
