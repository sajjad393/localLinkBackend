package httpapi

import (
	"encoding/base64"
	"strings"
)

// Bundle 236: an account made offline goes up to the server by itself - no
// password sheet and no pairing code. (234 B3, moving a known phone to the
// new account, is NOT here: 235 "one phone = one account" stays.)

// lockPlaceholder236 only passes the shared name / device checks of
// normalizeAuthInput in lock mode; it is never hashed or stored.
const lockPlaceholder236 = "lock-mode-236"

type registerLock struct {
	salt, hash []byte
	iterations int
}

// registerLock236 (B2): with no plain password, an account made offline may
// send its saved salt + hash (never the password). Only with a valid
// offline id. ok=false = bad input. lockMode=false = the normal password path.
func registerLock236(password, hashB64, saltB64 string, iterations int, offline string) (registerLock, bool, bool) {
	hashB64, saltB64 = strings.TrimSpace(hashB64), strings.TrimSpace(saltB64)
	if hashB64 == "" && saltB64 == "" {
		return registerLock{}, false, true
	}
	if password != "" || offline == "" {
		return registerLock{}, false, false
	}
	salt, e1 := base64.RawURLEncoding.DecodeString(strings.TrimRight(saltB64, "="))
	hash, e2 := base64.RawURLEncoding.DecodeString(strings.TrimRight(hashB64, "="))
	if e1 != nil || e2 != nil || len(salt) != passwordSaltLength || len(hash) != passwordKeyLength ||
		iterations < minResetIterations || iterations > maxResetIterations {
		return registerLock{}, false, false
	}
	return registerLock{salt: salt, hash: hash, iterations: iterations}, true, true
}
