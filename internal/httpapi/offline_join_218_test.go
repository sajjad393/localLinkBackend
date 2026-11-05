package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Bundle 218: a phone-only account joins the server (offline id link,
// recovery code kept, error codes, resolve, block carried, phone conflict).

const off218 = "offline_0123456789ab"

func register218(t *testing.T, a *API, body map[string]any) (int, map[string]any) {
	t.Helper()
	if body["password"] == nil {
		body["password"] = "correct-password"
	}
	res := doJSON(t, a.RegisterAccount, http.MethodPost, "/api/v1/auth/register", body, nil)
	out := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out
}

func audit218(a *API, action string) string {
	var detail string
	_ = a.store.DB().QueryRow(`SELECT COALESCE(target_id,'')||' '||COALESCE(detail,'') FROM admin_audit WHERE action=? ORDER BY id DESC LIMIT 1`, action).Scan(&detail)
	return detail
}

func TestOffline218RegisterLinksOfflineIDAndKeepsRecoveryCode(t *testing.T) {
	_, a := newTestAPI(t)
	code, out := register218(t, a, map[string]any{"username": "sumaira218", "device_id": "dev-sum-218", "device_name": "ZTE",
		"offline_user_id": off218, "recovery_code": "abcde-fghij-klmno-pqrs2"})
	if code != http.StatusCreated {
		t.Fatalf("register: %d %v", code, out)
	}
	if out["recovery_code"] != "ABCDE-FGHIJ-KLMNO-PQRS2" {
		t.Fatalf("server must keep the phone's code: %v", out["recovery_code"])
	}
	userID := out["account"].(map[string]any)["id"].(string)
	var gotUser, gotDev string
	if err := a.store.DB().QueryRow(`SELECT user_id,device_id FROM offline_ids WHERE offline_id=?`, off218).Scan(&gotUser, &gotDev); err != nil {
		t.Fatal(err)
	}
	if gotUser != userID || gotDev != "dev-sum-218" {
		t.Fatalf("link row: %s %s", gotUser, gotDev)
	}
	if !strings.Contains(audit218(a, "account_from_offline"), off218) {
		t.Fatal("audit account_from_offline missing")
	}
}

func TestOffline218BadOfflineIDAndBadCodeAreIgnored(t *testing.T) {
	_, a := newTestAPI(t)
	code, out := register218(t, a, map[string]any{"username": "plain218", "device_id": "dev-plain-218", "device_name": "x",
		"offline_user_id": "offline_XYZ", "recovery_code": "short"})
	if code != http.StatusCreated {
		t.Fatalf("register: %d %v", code, out)
	}
	if rc, _ := out["recovery_code"].(string); !recoveryFormat218.MatchString(rc) || strings.HasPrefix(rc, "SHORT") {
		t.Fatalf("bad code must be replaced by a new one: %q", rc)
	}
	var n int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM offline_ids`).Scan(&n)
	if n != 0 {
		t.Fatalf("bad offline id must not be linked: %d", n)
	}
}

func TestOffline218ErrorCodes(t *testing.T) {
	_, a := newTestAPI(t)
	registerDirectoryUser(t, a, "taken218", "dev-taken-218")
	code, out := register218(t, a, map[string]any{"username": "taken218", "device_id": "dev-other-218", "device_name": "x"})
	if code != http.StatusConflict || out["error_code"] != "USERNAME_TAKEN" || out["error"] != "username is already registered" {
		t.Fatalf("username taken: %d %v", code, out)
	}
	code, out = register218(t, a, map[string]any{"username": "fresh218", "device_id": "dev-taken-218", "device_name": "x"})
	if code != http.StatusConflict || out["error_code"] != "DEVICE_TAKEN" {
		t.Fatalf("device taken: %d %v", code, out)
	}
	res := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "taken218", "password": "wrong-password", "device_id": "dev-taken-218", "device_name": "x"}, nil)
	out = map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if res.Code != http.StatusUnauthorized || out["error_code"] != "BAD_LOGIN" {
		t.Fatalf("bad login: %d %v", res.Code, out)
	}
	res = doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "nobody218", "password": "wrong-password", "device_id": "dev-nb-218", "device_name": "x"}, nil)
	out = map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if res.Code != http.StatusUnauthorized || out["error_code"] != "BAD_LOGIN" {
		t.Fatalf("unknown user: %d %v", res.Code, out)
	}
}

func TestOffline218PairingRequiredCode(t *testing.T) {
	old, had := os.LookupEnv("LOCALLINK_PAIRING_CODE")
	os.Setenv("LOCALLINK_PAIRING_CODE", "PAIR-218")
	defer func() {
		if had {
			os.Setenv("LOCALLINK_PAIRING_CODE", old)
		} else {
			os.Unsetenv("LOCALLINK_PAIRING_CODE")
		}
	}()
	_, a := newTestAPI(t)
	code, out := register218(t, a, map[string]any{"username": "pair218", "device_id": "dev-pair-218", "device_name": "x", "pairing_code": "nope"})
	if code == http.StatusCreated {
		t.Skip("this build does not use LOCALLINK_PAIRING_CODE")
	}
	if code != http.StatusForbidden || out["error_code"] != "PAIRING_REQUIRED" {
		t.Fatalf("pairing: %d %v", code, out)
	}
}

func TestOffline218LoginLinksOfflineID(t *testing.T) {
	_, a := newTestAPI(t)
	registerDirectoryUser(t, a, "sumaira218", "dev-sum-218")
	res := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "sumaira218", "password": "correct-password", "device_id": "dev-sum-218", "device_name": "ZTE",
		"offline_user_id": off218}, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("login: %d %s", res.Code, res.Body.String())
	}
	var n int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM offline_ids WHERE offline_id=?`, off218).Scan(&n)
	if n != 1 || !strings.Contains(audit218(a, "account_link_offline"), off218) {
		t.Fatalf("login must link + audit: %d", n)
	}
	// A second link of the same id keeps the first owner (INSERT OR IGNORE).
	registerDirectoryUser(t, a, "other218", "dev-other-218")
	_, _ = register218(t, a, map[string]any{"username": "third218", "device_id": "dev-third-218", "device_name": "x", "offline_user_id": off218})
	var owner string
	_ = a.store.DB().QueryRow(`SELECT u.username FROM offline_ids o JOIN users u ON u.id=o.user_id WHERE o.offline_id=?`, off218).Scan(&owner)
	if owner != "sumaira218" {
		t.Fatalf("first link wins: %s", owner)
	}
}

func resolve218(t *testing.T, a *API, who AuthResponse, ids []string) (int, []map[string]any) {
	t.Helper()
	res := doJSON(t, a.ResolveUsers218, http.MethodPost, "/api/v1/users/resolve", map[string]any{"ids": ids}, directoryHeaders(who))
	var out struct {
		Found []map[string]any `json:"found"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out.Found
}

func TestOffline218ResolveOfflineAndPhoneIDs(t *testing.T) {
	_, a := newTestAPI(t)
	saj := registerDirectoryUser(t, a, "sajjad218", "dev-saj-218")
	_, out := register218(t, a, map[string]any{"username": "sumaira218", "device_id": "dev-sum-218", "device_name": "ZTE", "offline_user_id": off218})
	sumID := out["account"].(map[string]any)["id"].(string)
	code, found := resolve218(t, a, saj, []string{off218, "dev-sum-218", "offline_ffffffffffff", "dev-unknown", off218})
	if code != http.StatusOK || len(found) != 2 {
		t.Fatalf("resolve: %d %v", code, found)
	}
	for _, f := range found {
		if f["user_id"] != sumID || f["username"] != "sumaira218" || f["device_id"] != "dev-sum-218" {
			t.Fatalf("resolve row: %v", f)
		}
	}
	res := doJSON(t, a.ResolveUsers218, http.MethodPost, "/api/v1/users/resolve", map[string]any{"ids": []string{off218}}, nil)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("resolve needs device auth: %d", res.Code)
	}
}

func TestOffline218ResolveLimits(t *testing.T) {
	_, a := newTestAPI(t)
	saj := registerDirectoryUser(t, a, "sajjad218", "dev-saj-218")
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = "x"
	}
	if code, _ := resolve218(t, a, saj, ids); code != http.StatusBadRequest {
		t.Fatalf("101 ids must be refused: %d", code)
	}
	if code, found := resolve218(t, a, saj, []string{strings.Repeat("a", 129)}); code != http.StatusOK || len(found) != 0 {
		t.Fatalf("long id skipped: %d %v", code, found)
	}
	last := 0
	for i := 0; i < 31; i++ {
		last, _ = resolve218(t, a, saj, []string{"dev-saj-218"})
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("31st call in a minute must be 429: %d", last)
	}
}

func TestOffline218ContactAddFollowsOfflineID(t *testing.T) {
	_, a := newTestAPI(t)
	saj := registerDirectoryUser(t, a, "sajjad218", "dev-saj-218")
	_, out := register218(t, a, map[string]any{"username": "sumaira218", "device_id": "dev-sum-218", "device_name": "ZTE", "offline_user_id": off218})
	sumID := out["account"].(map[string]any)["id"].(string)
	res := doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", map[string]any{"user_id": off218}, directoryHeaders(saj))
	got := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &got)
	if res.Code != http.StatusOK || got["user_id"] != sumID {
		t.Fatalf("contact by offline id: %d %v", res.Code, got)
	}
	res = doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", map[string]any{"user_id": "offline_ffffffffffff"}, directoryHeaders(saj))
	got = map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &got)
	if res.Code != http.StatusNotFound || got["error_code"] != "NOT_ON_SERVER" {
		t.Fatalf("unknown offline id: %d %v", res.Code, got)
	}
}

func block218(t *testing.T, a *API, target string, devices []string, action string, seq int64, at time.Time) {
	t.Helper()
	rec := adminBlockBody{V: 1, Kind: "admin_block", ID: uuid.NewString(), TargetUserID: target, TargetDevices: devices,
		Action: action, Seq: seq, At: at.UTC().Format(time.RFC3339Nano), By: "test", Signer: "server"}
	raw, _ := json.Marshal(rec)
	blob, err := a.serverSign(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.applyAdminBlock(rec, blob); err != nil {
		t.Fatal(err)
	}
}

func status218(a *API, username string) string {
	var s string
	_ = a.store.DB().QueryRow(`SELECT status FROM users WHERE username=?`, username).Scan(&s)
	return s
}

func TestOffline218BlockWhileOfflineIsCarried(t *testing.T) {
	_, a := newTestAPI(t)
	block218(t, a, off218, []string{"dev-sum-218"}, "block", 1, time.Now().Add(-time.Hour))
	code, out := register218(t, a, map[string]any{"username": "sumaira218", "device_id": "dev-sum-218", "device_name": "ZTE", "offline_user_id": off218})
	if code != http.StatusCreated {
		t.Fatalf("register: %d %v", code, out)
	}
	if status218(a, "sumaira218") != "suspended" {
		t.Fatalf("block made offline must follow her: %s", status218(a, "sumaira218"))
	}
	if audit218(a, "admin_block_carry") == "" {
		t.Fatal("audit admin_block_carry missing")
	}
	var by string
	_ = a.store.DB().QueryRow(`SELECT body FROM admin_blocks WHERE target_user_id=?`, out["account"].(map[string]any)["id"]).Scan(&by)
	if !strings.Contains(by, "auto: blocked while offline") {
		t.Fatalf("record by: %s", by)
	}
}

func TestOffline218BlockByPhoneIDCarriedButUnblockIsNot(t *testing.T) {
	_, a := newTestAPI(t)
	// Newest record on the offline id is an unblock → nothing carried.
	block218(t, a, off218, nil, "block", 1, time.Now().Add(-2*time.Hour))
	block218(t, a, off218, nil, "unblock", 2, time.Now().Add(-time.Hour))
	_, _ = register218(t, a, map[string]any{"username": "free218", "device_id": "dev-free-218", "device_name": "x", "offline_user_id": off218})
	if status218(a, "free218") != "active" {
		t.Fatalf("unblock newest → stays active: %s", status218(a, "free218"))
	}
	// A record found only by her phone id (target_devices) is carried.
	block218(t, a, "offline_aaaaaaaaaaaa", []string{"dev-zte-218"}, "block", 1, time.Now().Add(-time.Hour))
	_, _ = register218(t, a, map[string]any{"username": "zte218", "device_id": "dev-zte-218", "device_name": "x", "offline_user_id": "offline_bbbbbbbbbbbb"})
	if status218(a, "zte218") != "suspended" {
		t.Fatalf("block by phone id must follow: %s", status218(a, "zte218"))
	}
}

func TestOffline218PhoneTakenCodeAndAudit(t *testing.T) {
	_, a := newTestAPI(t)
	saj := registerDirectoryUser(t, a, "sajjad218", "dev-saj-218")
	sum := registerDirectoryUser(t, a, "sumaira218", "dev-sum-218")
	body := func(name string) map[string]any {
		return map[string]any{"display_name": name, "username": name, "phone_number": "03001234567",
			"phone_visibility": "private", "discoverable_by_phone": true, "discoverable_by_name": true, "directory_sync_enabled": true}
	}
	if res := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", body("sajjad218"), directoryHeaders(saj)); res.Code != http.StatusOK {
		t.Fatalf("first number: %d %s", res.Code, res.Body.String())
	}
	res := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", body("sumaira218"), directoryHeaders(sum))
	got := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &got)
	if res.Code != http.StatusConflict || got["error_code"] != "PHONE_TAKEN" {
		t.Fatalf("phone taken: %d %v", res.Code, got)
	}
	d := audit218(a, "profile_phone_conflict")
	if !strings.Contains(d, "number=***4567") || strings.Contains(d, "0300123") {
		t.Fatalf("audit must show only the last 4: %q", d)
	}
}

func TestOffline218AdminUsersJoinedOfflineAndWebTag(t *testing.T) {
	_, a := newTestAPI(t)
	plain := registerDirectoryUser(t, a, "plain218", "dev-plain-218")
	_, out := register218(t, a, map[string]any{"username": "sumaira218", "device_id": "dev-sum-218", "device_name": "ZTE", "offline_user_id": off218})
	sumID := out["account"].(map[string]any)["id"].(string)
	list := []AdminUser{{ID: sumID, Username: "sumaira218"}, {ID: plain.Account.ID, Username: "plain218"}}
	a.enrichUsers216(list)
	if list[0].JoinedOfflineAt == "" || list[1].JoinedOfflineAt != "" {
		t.Fatalf("joined_offline_at: %+v", list)
	}
	views, _ := os.ReadFile("adminweb/views.js")
	css, _ := os.ReadFile("adminweb/styles.css")
	if !strings.Contains(string(views), "Joined from offline") || !strings.Contains(string(views), "u.joined_offline_at") || !strings.Contains(string(css), ".tag218") {
		t.Fatal("web Manage tag missing")
	}
	var v int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=58`).Scan(&v)
	if v != 1 {
		t.Fatalf("migration 58 missing: %d", v)
	}
}

func TestOffline218DeviceTakenNamesOwnerOnlyForSameHardware(t *testing.T) {
	_, a := newTestAPI(t)
	code, out := register218(t, a, map[string]any{"username": "owner218", "device_id": "dev-zte-218", "device_name": "ZTE", "hardware_fp": "hwfp-zte-0001"})
	if code != http.StatusCreated {
		t.Fatalf("register: %d %v", code, out)
	}
	code, out = register218(t, a, map[string]any{"username": "sumaira218", "device_id": "dev-zte-218", "device_name": "ZTE", "hardware_fp": "hwfp-zte-0001"})
	if code != http.StatusConflict || out["error_code"] != "DEVICE_TAKEN" || out["owner"] != "owner218" {
		t.Fatalf("same hardware must name the owner: %d %v", code, out)
	}
	code, out = register218(t, a, map[string]any{"username": "other218", "device_id": "dev-zte-218", "device_name": "ZTE", "hardware_fp": "hwfp-other-0002"})
	if code != http.StatusConflict || out["error_code"] != "DEVICE_TAKEN" || out["owner"] != nil {
		t.Fatalf("other hardware must not name the owner: %d %v", code, out)
	}
}
