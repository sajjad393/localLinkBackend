package httpapi

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Bundle 238: an offline row not seen for 30 days is marked hidden (the web
// shows it only under "Show hidden (N)"); a fresh one is not. Nothing is
// deleted before the 60 days.
func TestB238OldOfflineRowIsHidden(t *testing.T) {
	a := newAPI222(t)
	old := time.Now().UTC().Add(-40 * 24 * time.Hour).Format(time.RFC3339Nano)
	fresh := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	for _, r := range [][]string{{"dev-old-238", "oldone", old}, {"dev-new-238", "newone", fresh}} {
		if _, err := a.store.DB().Exec(`INSERT INTO seen_offline_accounts(device_id,username,name,phone_model,seen_by_device,link,first_seen,last_seen,carriers) VALUES(?,?,?,?,?,?,?,?,?)`,
			r[0], r[1], r[1], "ZTE", "rep", "router", r[2], r[2], ""); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]bool{}
	for _, x := range a.notOnServer222() {
		got[x.DeviceID] = x.Hidden
	}
	if len(got) != 2 || !got["dev-old-238"] || got["dev-new-238"] {
		t.Fatalf("hidden flags wrong: %v", got)
	}
}

func TestB238WebHasShowHiddenSwitch(t *testing.T) {
	b, err := os.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"data-users-hidden238", "Show hidden (", "nosAll.filter((x) => !x.hidden)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("views.js misses %q", want)
		}
	}
}
