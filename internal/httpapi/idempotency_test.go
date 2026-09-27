package httpapi

import "testing"

func TestValidIdempotencyKey(t *testing.T) {
	valid := []string{"abc-123", "device:operation_01", "A.B_c-9"}
	for _, value := range valid {
		if !validIdempotencyKey(value) {
			t.Fatalf("expected valid idempotency key %q", value)
		}
	}
	invalid := []string{"", "has space", "line\nbreak", "slash/value", string(make([]byte, 129))}
	for _, value := range invalid {
		if validIdempotencyKey(value) {
			t.Fatalf("expected invalid idempotency key %q", value)
		}
	}
}

func TestIdempotencyResponseEncryptionRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	plain := []byte(`{"token":"sensitive-response","ok":true}`)
	nonce, ciphertext, err := encryptIdempotencyBody(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decryptIdempotencyBody(key, nonce, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(plain) {
		t.Fatalf("round-trip mismatch: %q != %q", decoded, plain)
	}
	wrong := append([]byte(nil), key...)
	wrong[0] ^= 0xff
	if _, err := decryptIdempotencyBody(wrong, nonce, ciphertext); err == nil {
		t.Fatal("wrong encryption key unexpectedly decrypted stored response")
	}
}
