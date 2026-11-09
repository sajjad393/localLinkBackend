package httpapi

import (
	"crypto/ed25519"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// Bundle 243 (audit S3 A): with HTTPS on, ?proof=2 gets a v2 proof that
// also covers the HTTPS port and the certificate hash.

func TestPairingProofV2CoversTLS243(t *testing.T) {
	_, a := newTestAPI(t)
	sha := strings.Repeat("ab", 32)
	a.SetTLS243(8443, sha)
	nonce := newNonce231(t, 32)
	code, out := pairing231(t, a, "?nonce="+nonce+"&proof=2")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if out["proof_v"] != float64(2) || out["tls_port"] != float64(8443) || out["tls_cert_sha256"] != sha {
		t.Fatalf("v2 fields missing: %v", out)
	}
	pub, _ := unb64url(out["server_key"].(string))
	sig, _ := unb64url(out["proof"].(string))
	msg := func(port int, s string) []byte {
		return []byte(pairingProofMessage243(out["server_id"].(string), out["fingerprint"].(string), nonce, out["ts"].(string), port, s))
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), msg(8443, sha), sig) {
		t.Fatal("v2 proof must verify")
	}
	if ed25519.Verify(ed25519.PublicKey(pub), msg(8443, strings.Repeat("cd", 32)), sig) {
		t.Fatal("a swapped certificate hash must fail")
	}
	if ed25519.Verify(ed25519.PublicKey(pub), msg(9443, sha), sig) {
		t.Fatal("a swapped port must fail")
	}
	if !strings.HasPrefix(string(msg(1, "x")), "ll-pair-v2|") || strconv.Itoa(8443) == "" {
		t.Fatal("prefix")
	}
}

func TestPairingProofStaysV1ForOldApps243(t *testing.T) {
	_, a := newTestAPI(t)
	a.SetTLS243(8443, strings.Repeat("ab", 32))
	nonce := newNonce231(t, 32)
	_, out := pairing231(t, a, "?nonce="+nonce)
	if _, ok := out["tls_port"]; ok {
		t.Fatalf("old app (no proof=2) must get the v1 answer: %v", out)
	}
	if !verifyProof231(out, nonce) {
		t.Fatal("v1 proof must still verify")
	}
	// No HTTPS on: proof=2 still gets v1.
	_, b := newTestAPI(t)
	_, out2 := pairing231(t, b, "?nonce="+nonce+"&proof=2")
	if _, ok := out2["proof_v"]; ok || !verifyProof231(out2, nonce) {
		t.Fatalf("without HTTPS the answer stays v1: %v", out2)
	}
}
