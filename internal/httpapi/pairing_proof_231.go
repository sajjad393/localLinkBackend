package httpapi

import (
	"crypto/ed25519"
	"net/http"
	"strings"
	"time"
)

// Bundle 231 (audit S2): the server proves it holds its signing key.
//
// GET /api/v1/pairing/info?nonce=<base64url, 16..64 bytes> adds:
//
//	server_key  Ed25519 public key (the same key as GET /api/v1/trust)
//	key_id      serverKeyID(server_key)
//	ts          server time, RFC3339 UTC
//	proof       base64url Ed25519 signature over
//	            "ll-pair-v1|<server_id>|<fingerprint>|<nonce>|<ts>"
//
// Without a nonce the answer is exactly as before (old apps unaffected).
// Every other server signature is over a JSON body ("{..."), so a pairing
// proof can never be taken for one of them; the nonce is base64url only, so
// it can't add a "|".
const pairingProofPrefix231 = "ll-pair-v1"

func pairingProofMessage231(serverID, fingerprint, nonce, ts string) string {
	return pairingProofPrefix231 + "|" + serverID + "|" + fingerprint + "|" + nonce + "|" + ts
}

// pairingNonce231 returns (nonce, present, valid).
func pairingNonce231(r *http.Request) (string, bool, bool) {
	q := r.URL.Query()
	if !q.Has("nonce") {
		return "", false, true
	}
	nonce := strings.TrimSpace(q.Get("nonce"))
	if len(nonce) > 128 {
		return "", true, false
	}
	for _, c := range nonce {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", true, false
		}
	}
	raw, err := unb64url(nonce)
	if err != nil || len(raw) < 16 || len(raw) > 64 {
		return "", true, false
	}
	return nonce, true, true
}

func (a *API) addPairingProof231(info *PairingInfo, nonce string) error {
	key, err := a.serverSigningKey()
	if err != nil {
		return err
	}
	pub := key.Public().(ed25519.PublicKey)
	ts := time.Now().UTC().Format(time.RFC3339)
	info.ServerKey = b64url(pub)
	info.KeyID = serverKeyID(pub)
	info.ProofTS = ts
	info.Proof = b64url(ed25519.Sign(key, []byte(pairingProofMessage231(info.ServerID, info.Fingerprint, nonce, ts))))
	return nil
}
