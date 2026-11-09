package httpapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Bundle 231 (audit S2): the pairing answer carries a proof made with the
// server signing key when the phone sends a nonce.

func newNonce231(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b64url(b)
}

func pairing231(t *testing.T, a *API, query string) (int, map[string]any) {
	t.Helper()
	res := doJSON(t, a.PairingInfo, http.MethodGet, "/api/v1/pairing/info"+query, nil, nil)
	out := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return res.Code, out
}

func verifyProof231(out map[string]any, nonce string) bool {
	pub, err := unb64url(out["server_key"].(string))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := unb64url(out["proof"].(string))
	if err != nil {
		return false
	}
	msg := pairingProofMessage231(out["server_id"].(string), out["fingerprint"].(string), nonce, out["ts"].(string))
	return ed25519.Verify(ed25519.PublicKey(pub), []byte(msg), sig)
}

func TestPairingProofVerifiesWithServerKey231(t *testing.T) {
	_, a := newTestAPI(t)
	nonce := newNonce231(t, 32)
	code, out := pairing231(t, a, "?nonce="+nonce)
	if code != http.StatusOK {
		t.Fatalf("status %d %v", code, out)
	}
	for _, k := range []string{"server_key", "key_id", "ts", "proof", "server_id", "fingerprint"} {
		if s, _ := out[k].(string); s == "" {
			t.Fatalf("missing %s: %v", k, out)
		}
	}
	if !verifyProof231(out, nonce) {
		t.Fatal("proof must verify with server_key and our nonce")
	}
	// The same key as GET /api/v1/trust (the admin trust key).
	res := doJSON(t, a.Trust, http.MethodGet, "/api/v1/trust", nil, nil)
	var trust map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &trust)
	if trust["server_key"] != out["server_key"] || trust["key_id"] != out["key_id"] {
		t.Fatal("pairing server_key must equal the trust key")
	}
}

func TestPairingProofFailsForOtherNonceOrTamper231(t *testing.T) {
	_, a := newTestAPI(t)
	nonce := newNonce231(t, 32)
	_, out := pairing231(t, a, "?nonce="+nonce)
	if verifyProof231(out, newNonce231(t, 32)) {
		t.Fatal("a different nonce (replay) must fail")
	}
	bad := map[string]any{}
	for k, v := range out {
		bad[k] = v
	}
	bad["fingerprint"] = strings.Repeat("A", 64)
	if verifyProof231(bad, nonce) {
		t.Fatal("a changed fingerprint must fail")
	}
	// Another machine with its own key can't make a proof for this key.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	forged := map[string]any{}
	for k, v := range out {
		forged[k] = v
	}
	forged["proof"] = b64url(ed25519.Sign(other, []byte(pairingProofMessage231(out["server_id"].(string), out["fingerprint"].(string), nonce, out["ts"].(string)))))
	if verifyProof231(forged, nonce) {
		t.Fatal("a proof from another key must fail")
	}
}

func TestPairingWithoutNonceKeepsOldShape231(t *testing.T) {
	_, a := newTestAPI(t)
	code, out := pairing231(t, a, "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for _, k := range []string{"server_key", "key_id", "ts", "proof"} {
		if _, ok := out[k]; ok {
			t.Fatalf("old shape must not have %s", k)
		}
	}
	for _, k := range []string{"service", "version", "server_id", "fingerprint", "name", "pairing_required"} {
		if _, ok := out[k]; !ok {
			t.Fatalf("old field %s missing", k)
		}
	}
	if len(out) != 6 {
		t.Fatalf("old shape has exactly 6 fields, got %d: %v", len(out), out)
	}
}

func TestPairingBadNonceRefused231(t *testing.T) {
	_, a := newTestAPI(t)
	for _, q := range []string{
		"?nonce=",
		"?nonce=" + newNonce231(t, 8),
		"?nonce=" + newNonce231(t, 65),
		"?nonce=abc%7Cdef0123456789abcdef",
		"?nonce=" + strings.Repeat("A", 200),
	} {
		code, out := pairing231(t, a, q)
		if code != http.StatusBadRequest || out["error_code"] != "BAD_NONCE" {
			t.Fatalf("%s: want 400 BAD_NONCE, got %d %v", q, code, out)
		}
	}
	if code, _ := pairing231(t, a, "?nonce="+newNonce231(t, 16)); code != http.StatusOK {
		t.Fatal("16 bytes is allowed")
	}
	if code, _ := pairing231(t, a, "?nonce="+newNonce231(t, 64)); code != http.StatusOK {
		t.Fatal("64 bytes is allowed")
	}
}

func TestPairingProofIsNotJSON231(t *testing.T) {
	// Every other signed body is JSON; the proof string can't parse as one.
	msg := pairingProofMessage231("id", "FP", "bm9uY2U", "2026-11-09T00:00:00Z")
	var v any
	if json.Unmarshal([]byte(msg), &v) == nil {
		t.Fatal("proof message must never be valid JSON")
	}
	if !strings.HasPrefix(msg, "ll-pair-v1|") {
		t.Fatal("prefix")
	}
}
