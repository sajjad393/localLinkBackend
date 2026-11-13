package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Bundle 279: web Users must not show an account twice ("Active" + "Not on
// server yet") once its phone joined or its offline id was published.

func notOnServer279(t *testing.T, a *API) string {
	t.Helper()
	res := doJSON(t, a.AdminUsers, http.MethodGet, "/api/v1/admin/users?limit=50&include=not_on_server", nil, map[string]string{"X-Admin-Token": "admin-secret"})
	if res.Code != http.StatusOK {
		t.Fatalf("admin users: %d %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	i := strings.Index(body, `"not_on_server":`)
	if i < 0 {
		t.Fatalf("no not_on_server list: %s", body)
	}
	return body[i:]
}

func TestB279PublishedOfflineIdIsNotReported(t *testing.T) {
	a := newAPI222(t)
	ali := register222(t, a, "ali", "ali-password-1", "ali-phone")
	kiran := register222(t, a, "kiran", "kiran-password-1", "kiran-phone")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// kiran's offline id was linked at publish, but the card ali saw carries
	// another phone id (so the joined-phone rule alone does not catch it).
	if _, err := a.store.DB().Exec(`INSERT INTO offline_ids(offline_id,user_id,device_id,linked_at) VALUES(?,?,?,?)`, "offline_457370aaaaaa", kiran.Account.ID, "kiran-phone", now); err != nil {
		t.Fatal(err)
	}
	a.noteOfflineSeen222("ali-phone", []offlineSeen222{
		{DeviceID: "kiran-old-phone-id", UserID: "offline_457370aaaaaa", Username: "kiran", Link: "mesh"},
		{DeviceID: "her-phone", UserID: "offline_0123456789ab", Username: "sumaira", Link: "nearby"},
	})
	list := notOnServer279(t, a)
	if strings.Contains(list, `"username":"kiran"`) {
		t.Fatalf("a published offline id must not be listed: %s", list)
	}
	if !strings.Contains(list, `"username":"sumaira"`) {
		t.Fatalf("a real not-yet-joined account is still listed: %s", list)
	}
	_ = ali
}

func TestB279OldRowsVanishAtOnce(t *testing.T) {
	a := newAPI222(t)
	register222(t, a, "ali", "ali-password-1", "ali-phone")
	old := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	for _, r := range [][2]string{{"kiran-phone", "kiran"}, {"sum-phone", "sumaira"}, {"zara-phone", "zara"}} {
		if _, err := a.store.DB().Exec(`INSERT INTO seen_offline_accounts(device_id,username,name,phone_model,seen_by_device,link,first_seen,last_seen,carriers) VALUES(?,?,'','','ali-phone','mesh',?,?,'')`, r[0], r[1], old, old); err != nil {
			t.Fatal(err)
		}
	}
	// kiran's phone joined later; sumaira's offline id was linked from her phone.
	register222(t, a, "kiran", "kiran-password-1", "kiran-phone")
	sum := register222(t, a, "sumaira", "sumaira-password-1", "sum-phone-new")
	if _, err := a.store.DB().Exec(`INSERT INTO offline_ids(offline_id,user_id,device_id,linked_at) VALUES(?,?,?,?)`, "offline_b6d2d4aaaaaa", sum.Account.ID, "sum-phone", old); err != nil {
		t.Fatal(err)
	}
	list := notOnServer279(t, a)
	if strings.Contains(list, `"username":"kiran"`) || strings.Contains(list, `"username":"sumaira"`) {
		t.Fatalf("rows from before the join must not be listed: %s", list)
	}
	if !strings.Contains(list, `"username":"zara"`) {
		t.Fatalf("a real not-yet-joined account is still listed: %s", list)
	}
	var n int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM seen_offline_accounts`).Scan(&n)
	if n != 3 {
		t.Fatalf("read-time filter only, rows kept: %d", n)
	}
}
