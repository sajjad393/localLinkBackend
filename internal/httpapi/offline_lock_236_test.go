package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Bundle 236: an account made offline goes up by itself - saved password
// lock (B2), no pairing code (B4). 235 (one phone = one account) stays.

const off236 = "offline_0123456789cd"

func setPairing236(t *testing.T, code string) {
	t.Helper()
	old, had := os.LookupEnv("LOCALLINK_PAIRING_CODE")
	os.Setenv("LOCALLINK_PAIRING_CODE", code)
	t.Cleanup(func() {
		if had {
			os.Setenv("LOCALLINK_PAIRING_CODE", old)
		} else {
			os.Unsetenv("LOCALLINK_PAIRING_CODE")
		}
	})
}

func lock236(password string, iterations int) (string, string) {
	salt := make([]byte, passwordSaltLength)
	_, _ = rand.Read(salt)
	hash := derivePasswordKey(password, salt, iterations)
	return base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(hash)
}

func login236(t *testing.T, a *API, user, password, device string) int {
	t.Helper()
	res := doJSON(t, a.LoginAccount, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": user, "password": password, "device_id": device, "device_name": "Phone"}, nil)
	return res.Code
}

func TestB236LockModeRegisterThenRealPasswordSignsIn(t *testing.T) {
	_, a := newTestAPI(t)
	salt, hash := lock236("her-real-password", 1000)
	code, out := register218(t, a, map[string]any{"username": "lock236", "password": "", "device_id": "dev-lock-236", "device_name": "Note",
		"offline_user_id": off236, "password_salt": salt, "password_hash": hash, "password_iterations": 1000})
	if code != http.StatusCreated {
		t.Fatalf("lock register: %d %v", code, out)
	}
	if login236(t, a, "lock236", "her-real-password", "dev-lock-236") != http.StatusOK {
		t.Fatal("the real password must sign in after a lock register")
	}
	if login236(t, a, "lock236", "wrong-password-1", "dev-lock-236") == http.StatusOK {
		t.Fatal("a wrong password must not sign in")
	}
	if !strings.Contains(audit218(a, "account_register_lock"), "mode=lock") {
		t.Fatal("audit line for the lock register missing")
	}
	// The 230 re-hash moves the rounds up at the login.
	var it int
	_ = a.store.DB().QueryRow(`SELECT password_iterations FROM users WHERE username='lock236'`).Scan(&it)
	if it != passwordIterations {
		t.Fatalf("re-hash to %d expected after login, got %d", passwordIterations, it)
	}
}

func TestB236LockModeNeedsOfflineIDAndGoodData(t *testing.T) {
	_, a := newTestAPI(t)
	salt, hash := lock236("her-real-password", 1000)
	cases := []map[string]any{
		{"username": "nolock1", "password": "", "device_id": "d1", "device_name": "x", "password_salt": salt, "password_hash": hash, "password_iterations": 1000},
		{"username": "nolock2", "password": "plain-password-1", "device_id": "d2", "device_name": "x", "offline_user_id": off236, "password_salt": salt, "password_hash": hash, "password_iterations": 1000},
		{"username": "nolock3", "password": "", "device_id": "d3", "device_name": "x", "offline_user_id": off236, "password_salt": salt, "password_hash": "abc", "password_iterations": 1000},
		{"username": "nolock4", "password": "", "device_id": "d4", "device_name": "x", "offline_user_id": off236, "password_salt": salt, "password_hash": hash, "password_iterations": 10},
	}
	for i, c := range cases {
		if code, out := register218(t, a, c); code != http.StatusBadRequest {
			t.Fatalf("case %d must be refused: %d %v", i, code, out)
		}
	}
	if code, _ := register218(t, a, map[string]any{"username": "nolock5", "password": "", "device_id": "d5", "device_name": "x", "offline_user_id": off236}); code != http.StatusBadRequest {
		t.Fatalf("empty password must still be refused: %d", code)
	}
}

// 235 stays: a known phone + lock mode still waits for proof.
func TestB236KnownPhoneStillDuplicate235(t *testing.T) {
	_, a := newTestAPI(t)
	if code, out := register218(t, a, map[string]any{"username": "oldali236", "device_id": "dev-note-236", "device_name": "Note", "hardware_fp": "hwfp-note-236"}); code != http.StatusCreated {
		t.Fatalf("old register: %d %v", code, out)
	}
	salt, hash := lock236("her-real-password", 1000)
	code, out := register218(t, a, map[string]any{"username": "newali236", "password": "", "device_id": "dev-note-236", "device_name": "Note",
		"hardware_fp": "hwfp-note-236", "offline_user_id": off236, "password_salt": salt, "password_hash": hash, "password_iterations": 1000})
	if code != http.StatusConflict || out["error_code"] != "DEVICE_TAKEN" || out["duplicate"] != true {
		t.Fatalf("known phone must still wait for proof (235): %d %v", code, out)
	}
}

func TestB236OfflineAccountSkipsPairingCode(t *testing.T) {
	setPairing236(t, "PAIR-236")
	_, a := newTestAPI(t)
	code, out := register218(t, a, map[string]any{"username": "pairoff236", "device_id": "dev-po-236", "device_name": "x", "offline_user_id": off236})
	if code != http.StatusCreated {
		t.Fatalf("offline account must not need the pairing code: %d %v", code, out)
	}
	code, out = register218(t, a, map[string]any{"username": "pairon236", "device_id": "dev-pn-236", "device_name": "x", "pairing_code": "nope"})
	if code != http.StatusForbidden || out["error_code"] != "PAIRING_REQUIRED" {
		t.Fatalf("an online sign-up still needs it: %d %v", code, out)
	}
	code, _ = register218(t, a, map[string]any{"username": "pairbad236", "device_id": "dev-pb-236", "device_name": "x", "offline_user_id": "not-an-offline-id"})
	if code != http.StatusForbidden {
		t.Fatalf("a bad offline id is not an offline account: %d", code)
	}
}

func TestB236CarriedBoxSkipsPairingCode(t *testing.T) {
	a := newAPI222(t)
	ali := register222(t, a, "ali", "ali-password-1", "ali-phone")
	setPairing236(t, "PAIR-236")
	her := newHer222(t)
	_, ans, _, raw := upload222(t, a, ali, "box-236", makeBox222(t, a, her, "box-236", "sumaira", "her-phone", "her-secret-pass", false))
	if ans.Status == "refused" {
		t.Fatalf("a carried box from an account made offline needs no pairing code: %s", raw)
	}
}

func TestB236LockRules(t *testing.T) {
	salt, hash := lock236("her-real-password", 1000)
	if _, mode, ok := registerLock236("", hash, salt, 3000000, off236); ok || mode {
		t.Fatal("too many rounds must be refused")
	}
	if _, mode, ok := registerLock236("", hash, salt, 1000, off236); !ok || !mode {
		t.Fatal("good lock refused")
	}
	if _, mode, ok := registerLock236("pw-123456789", "", "", 0, ""); !ok || mode {
		t.Fatal("the normal password path must stay")
	}
}
