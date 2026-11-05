package httpapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// Bundle 220: "llaa1" box = a small secret for ONE phone inside an admin
// action card. Every phone carries the card, only the target phone can open
// its box, so the other phones can't see who the action is for.
//
//	box   = base64url( epk(32) | nonce(12) | AES-256-GCM(ciphertext + tag 16) )
//	key   = HKDF-SHA256(ikm = X25519(eph, recipient), salt = epk|recipient,
//	        info = "llaa1|box", 32 bytes)
//	aad   = "llaa1"
//
// The app (lib/features/admin/data/actions/admin_action_box.dart) uses the
// same steps with package:cryptography.

const llaa1Info = "llaa1|box"
const llaa1AAD = "llaa1"

func hkdfSHA256(ikm, salt, info []byte, n int) []byte {
	if len(salt) == 0 {
		salt = make([]byte, sha256.Size)
	}
	ext := hmac.New(sha256.New, salt)
	ext.Write(ikm)
	prk := ext.Sum(nil)
	out := make([]byte, 0, n)
	var prev []byte
	for i := byte(1); len(out) < n; i++ {
		m := hmac.New(sha256.New, prk)
		m.Write(prev)
		m.Write(info)
		m.Write([]byte{i})
		prev = m.Sum(nil)
		out = append(out, prev...)
	}
	return out[:n]
}

func llaa1Key(shared, epk, recipient []byte) []byte {
	salt := append(append([]byte{}, epk...), recipient...)
	return hkdfSHA256(shared, salt, []byte(llaa1Info), 32)
}

// lockBox locks plain for the X25519 public key (base64url).
func lockBox(recipientB64 string, plain []byte) (string, error) {
	eph := make([]byte, 32)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, eph); err != nil {
		return "", err
	}
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return lockBoxFixed(eph, nonce, recipientB64, plain)
}

// lockBoxFixed is lockBox with a given one-time key and nonce (the shared
// Go / Dart test vector uses it).
func lockBoxFixed(ephPriv, nonce []byte, recipientB64 string, plain []byte) (string, error) {
	rpub, err := unb64url(recipientB64)
	if err != nil || len(rpub) != 32 {
		return "", errors.New("bad recipient key")
	}
	if len(nonce) != 12 {
		return "", errors.New("bad nonce")
	}
	curve := ecdh.X25519()
	recipient, err := curve.NewPublicKey(rpub)
	if err != nil {
		return "", err
	}
	eph, err := curve.NewPrivateKey(ephPriv)
	if err != nil {
		return "", err
	}
	shared, err := eph.ECDH(recipient)
	if err != nil {
		return "", err
	}
	epk := eph.PublicKey().Bytes()
	block, err := aes.NewCipher(llaa1Key(shared, epk, rpub))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, plain, []byte(llaa1AAD))
	out := append(append(append([]byte{}, epk...), nonce...), sealed...)
	return b64url(out), nil
}

// openBox opens a box with an X25519 private key (32-byte seed/scalar).
// The server never needs it in production (boxes are for phones); tests and
// the shared test vector use it.
func openBox(privateKey []byte, box string) ([]byte, error) {
	raw, err := unb64url(box)
	if err != nil || len(raw) < 32+12+16 {
		return nil, errors.New("bad box")
	}
	curve := ecdh.X25519()
	priv, err := curve.NewPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	epkBytes := raw[:32]
	epk, err := curve.NewPublicKey(epkBytes)
	if err != nil {
		return nil, err
	}
	shared, err := priv.ECDH(epk)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(llaa1Key(shared, epkBytes, priv.PublicKey().Bytes()))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, raw[32:44], raw[44:], []byte(llaa1AAD))
}
