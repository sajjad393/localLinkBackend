package httpapi

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Bundle 222: nearby phones carry an offline account to the server.

type herPhone222 struct {
	idPriv   *ecdh.PrivateKey
	signPriv ed25519.PrivateKey
	signPub  ed25519.PublicKey
	code     string
}

func newHer222(t *testing.T) herPhone222 {
	t.Helper()
	idPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return herPhone222{idPriv: idPriv, signPriv: priv, signPub: pub, code: "ABCDE-FGHIJ-KLMNO-PQRST"}
}

func recHash222(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}

// makeBox222 builds her join box exactly like the app: signed body, sealed
// for the server's join key from the signed join note.
func makeBox222(t *testing.T, a *API, her herPhone222, boxID, username, device, password string, tamper bool) string {
	t.Helper()
	salt := make([]byte, passwordSaltLength)
	_, _ = rand.Read(salt)
	body, _ := json.Marshal(joinBoxBody222{
		V: 1, BoxID: boxID, Username: username, DisplayName: "Sumaira Khan", OfflineUserID: "offline_0123456789ab",
		DeviceID: device, DeviceName: "ZTE Blade A35", Platform: "android",
		PwdSalt: b64url(salt), PwdHash: b64url(derivePasswordKey(password, salt, passwordIterations)), Iterations: passwordIterations,
		RecoveryHash: recHash222(her.code), IdentityKey: b64url(her.idPriv.PublicKey().Bytes()), SigningKey: b64url(her.signPub),
		MadeAt: "2026-11-06T10:00:00Z",
	})
	sig := ed25519.Sign(her.signPriv, body)
	if tamper {
		body = []byte(strings.Replace(string(body), username, username+"x", 1))
	}
	sealed, _ := json.Marshal(signedBlob{Body: string(body), Sig: b64url(sig)})
	_, joinKey, err := a.joinNote222()
	if err != nil {
		t.Fatal(err)
	}
	box, err := lockBox(joinKey, sealed)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func upload222(t *testing.T, a *API, carrier AuthResponse, boxID, box string) (int, joinAnswerBody222, signedBlob, string) {
	t.Helper()
	res := doJSON(t, a.JoinRelay222, http.MethodPost, "/api/v1/auth/join-relay", map[string]any{"box_id": boxID, "box": box},
		map[string]string{"X-Device-ID": carrier.Device.ID, "Authorization": "Bearer " + carrier.Token})
	var out struct {
		Answer signedBlob `json:"answer"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	var ans joinAnswerBody222
	_ = json.Unmarshal([]byte(out.Answer.Body), &ans)
	return res.Code, ans, out.Answer, res.Body.String()
}

func openToken222(t *testing.T, her herPhone222, box string) map[string]string {
	t.Helper()
	plain, err := openBox(her.idPriv.Bytes(), box)
	if err != nil {
		t.Fatalf("her phone must open the token box: %v", err)
	}
	var m map[string]string
	_ = json.Unmarshal(plain, &m)
	return m
}

func TestJoinRelayNewAccount222(t *testing.T) {
	a := newAPI222(t)
	ali := register222(t, a, "ali", "ali-password-1", "ali-phone")
	her := newHer222(t)
	box := makeBox222(t, a, her, "box-1", "sumaira", "her-phone", "her-secret-pass", false)
	if strings.Contains(box, "her-secret-pass") || len(box) > joinBoxMaxLen222 {
		t.Fatal("the box must be sealed and small")
	}
	code, ans, blob, raw := upload222(t, a, ali, "box-1", box)
	if code != http.StatusOK || ans.Status != "joined" || ans.Username != "sumaira" || ans.UserID == "" || ans.TokenBox == "" {
		t.Fatalf("join: %d %s", code, raw)
	}
	if strings.Contains(raw, "her-secret-pass") {
		t.Fatal("the answer must never hold her password")
	}
	if !a.serverVerify(blob) {
		t.Fatal("the answer must be server-signed")
	}
	tok := openToken222(t, her, ans.TokenBox)
	if tok["device_id"] != "her-phone" || !signedIn222(a, "her-phone", tok["token"]) {
		t.Fatalf("her phone must be signed in with the token in the box: %v", tok)
	}
	// The carrier can't use the token box (it is for her key only).
	other, _ := ecdh.X25519().GenerateKey(rand.Reader)
	if _, err := openBox(other.Bytes(), ans.TokenBox); err == nil {
		t.Fatal("another phone must not open her token")
	}
	// Her password works on the server, never sent.
	if c, _ := login222(t, a, "sumaira", "her-secret-pass", "her-phone"); c != http.StatusOK {
		t.Fatalf("her password must work: %d", c)
	}
	// Same box again (another carrier): same answer.
	zee := register222(t, a, "zeeshan", "zee-password-1", "zee-phone")
	code2, ans2, _, raw2 := upload222(t, a, zee, "box-1", box)
	if code2 != http.StatusOK || ans2.UserID != ans.UserID || ans2.At != ans.At || !strings.Contains(raw2, `"again":true`) {
		t.Fatalf("same box must give the same answer: %s", raw2)
	}
	var users int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM users WHERE username LIKE 'sumaira%'`).Scan(&users)
	if users != 1 {
		t.Fatalf("one account only: %d", users)
	}
}

func TestJoinRelayNameTakenRenames222(t *testing.T) {
	a := newAPI222(t)
	ali := register222(t, a, "ali", "ali-password-1", "ali-phone")
	register222(t, a, "sumaira", "someone-else-1", "other-phone")
	her := newHer222(t)
	_, ans, _, raw := upload222(t, a, ali, "box-2", makeBox222(t, a, her, "box-2", "sumaira", "her-phone", "her-secret-pass", false))
	if ans.Status != "renamed" || !regexp.MustCompile(`^sumaira\d{1,2}$`).MatchString(ans.Username) {
		t.Fatalf("name taken by someone else -> sumaira37: %s", raw)
	}
	if c, _ := login222(t, a, ans.Username, "her-secret-pass", "her-phone"); c != http.StatusOK {
		t.Fatalf("renamed account must work: %d", c)
	}
}

func TestJoinRelaySameRecoveryCodeLinks222(t *testing.T) {
	a := newAPI222(t)
	ali := register222(t, a, "ali", "ali-password-1", "ali-phone")
	first := register222(t, a, "sumaira", "her-secret-pass", "her-old-phone")
	her := newHer222(t)
	her.code = first.RecoveryCode // her own account on the server (218 keeps the code)
	_, ans, _, raw := upload222(t, a, ali, "box-3", makeBox222(t, a, her, "box-3", "sumaira", "her-new-phone", "her-secret-pass", false))
	if ans.Status != "linked" || ans.UserID != first.Account.ID || ans.Username != "sumaira" {
		t.Fatalf("same name + her recovery code = her account: %s", raw)
	}
}

func TestJoinRelayRefusesAndBadBoxes222(t *testing.T) {
	a := newAPI222(t)
	ali := register222(t, a, "ali", "ali-password-1", "ali-phone")
	register222(t, a, "zeeshan", "zee-password-1", "zee-phone")
	her := newHer222(t)
	// Phone id of someone else: stays local + audit.
	_, ans, _, raw := upload222(t, a, ali, "box-4", makeBox222(t, a, her, "box-4", "sumaira", "zee-phone", "her-secret-pass", false))
	if ans.Status != "refused" || ans.Reason != "device_taken" {
		t.Fatalf("phone taken: %s", raw)
	}
	// Changed on the way: refused.
	if c, _, _, _ := upload222(t, a, ali, "box-5", makeBox222(t, a, her, "box-5", "sumaira", "her-phone", "p-123456789", true)); c != http.StatusBadRequest {
		t.Fatalf("a changed box must be refused: %d", c)
	}
	// box_id must match the sealed body.
	if c, _, _, _ := upload222(t, a, ali, "other-id", makeBox222(t, a, her, "box-6", "sumaira", "her-phone", "p-123456789", false)); c != http.StatusBadRequest {
		t.Fatalf("box id mismatch: %d", c)
	}
	// No carrier sign-in: refused.
	res := doJSON(t, a.JoinRelay222, http.MethodPost, "/api/v1/auth/join-relay", map[string]any{"box_id": "b", "box": "x"})
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("carrier must be signed in: %d", res.Code)
	}
}

func TestJoinRelayAlreadyJoinedNoSignOut222(t *testing.T) {
	a := newAPI222(t)
	ali := register222(t, a, "ali", "ali-password-1", "ali-phone")
	her := newHer222(t)
	_, first, _, _ := upload222(t, a, ali, "box-7", makeBox222(t, a, her, "box-7", "sumaira", "her-phone", "her-secret-pass", false))
	tok := openToken222(t, her, first.TokenBox)
	// A late, re-made box from her phone: no new token while she is signed in.
	_, ans, _, raw := upload222(t, a, ali, "box-8", makeBox222(t, a, her, "box-8", "sumaira", "her-phone", "her-secret-pass", false))
	if ans.Status != "already" || ans.TokenBox != "" {
		t.Fatalf("already joined: %s", raw)
	}
	if !signedIn222(a, "her-phone", tok["token"]) {
		t.Fatal("a late answer must not sign her out")
	}
}

func TestJoinNoteIsStable222(t *testing.T) {
	a := newAPI222(t)
	r1 := doJSON(t, a.Trust, http.MethodGet, "/api/v1/trust", nil)
	r2 := doJSON(t, a.Trust, http.MethodGet, "/api/v1/trust", nil)
	if r1.Body.String() != r2.Body.String() || !strings.Contains(r1.Body.String(), `"join_note"`) || !strings.Contains(r1.Body.String(), `"join_key"`) {
		t.Fatalf("every phone must hand out the same note: %s", r1.Body.String())
	}
	var out struct {
		ServerKey string     `json:"server_key"`
		JoinNote  signedBlob `json:"join_note"`
	}
	_ = json.Unmarshal(r1.Body.Bytes(), &out)
	if !verifyWithKey(out.ServerKey, out.JoinNote) {
		t.Fatal("the note must be signed by the server key")
	}
}

func TestOfflineSeenInWebUsers222(t *testing.T) {
	a := newAPI222(t)
	ali := register222(t, a, "ali", "ali-password-1", "ali-phone")
	a.noteOfflineSeen222("ali-phone", []offlineSeen222{
		{DeviceID: "her-phone", UserID: "offline_0123456789ab", Username: "sumaira", Name: "Sumaira", Model: "ZTE Blade A35", Link: "wifi_direct", Carrying: true},
		{DeviceID: "ali-phone", UserID: "offline_aaaaaaaaaaaa", Username: "fake"}, // already on the server: skipped
		{DeviceID: "x", UserID: "real-user-id", Username: "notoffline"},           // not an offline id: skipped
	})
	res := doJSON(t, a.AdminUsers, http.MethodGet, "/api/v1/admin/users?limit=50&include=not_on_server", nil, map[string]string{"X-Admin-Token": "admin-secret"})
	body := res.Body.String()
	if res.Code != http.StatusOK || !strings.Contains(body, `"not_on_server":[`) || !strings.Contains(body, `"username":"sumaira"`) ||
		!strings.Contains(body, `"seen_by":"ali"`) || !strings.Contains(body, `"carriers":1`) || strings.Contains(body, "notoffline") || strings.Contains(body, `"fake"`) {
		t.Fatalf("web Users must list her as not on server yet: %s", body)
	}
	plain := doJSON(t, a.AdminUsers, http.MethodGet, "/api/v1/admin/users?limit=50", nil, map[string]string{"X-Admin-Token": "admin-secret"})
	if strings.Contains(plain.Body.String(), "not_on_server") {
		t.Fatal("old callers get the same answer as before")
	}
	her := newHer222(t)
	upload222(t, a, ali, "box-9", makeBox222(t, a, her, "box-9", "sumaira", "her-phone", "her-secret-pass", false))
	after := doJSON(t, a.AdminUsers, http.MethodGet, "/api/v1/admin/users?limit=50&include=not_on_server", nil, map[string]string{"X-Admin-Token": "admin-secret"})
	if strings.Contains(after.Body.String(), `"not_on_server":[{`) {
		t.Fatalf("after the join she is a normal user: %s", after.Body.String())
	}
	_ = base64.RawURLEncoding
}
