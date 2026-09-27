package httpapi

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	idempotencyTTL          = 24 * time.Hour
	idempotencyInFlightTTL  = 5 * time.Minute
	maxIdempotencyKeyLength = 128
	maxIdempotencyBodyStore = 256 << 10
)

type idempotencyWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
	wrote  bool
}

func newIdempotencyWriter() *idempotencyWriter {
	return &idempotencyWriter{header: make(http.Header), status: http.StatusOK}
}

func (w *idempotencyWriter) Header() http.Header { return w.header }
func (w *idempotencyWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
}
func (w *idempotencyWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(p)
}

func validIdempotencyKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxIdempotencyKeyLength || strings.ContainsAny(value, "\r\n") {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' || r == ':' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (a *API) idempotencyEncryptionKey() ([]byte, error) {
	var secret string
	err := a.store.DB().QueryRow(`SELECT identity_secret FROM server_identity WHERE singleton=1`).Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			return nil, err
		}
		secret = base64.RawURLEncoding.EncodeToString(secretBytes)
		serverID := uuid.NewString()
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO server_identity(singleton,server_id,identity_secret,created_at) VALUES(1,?,?,?)`, serverID, secret, now); err != nil {
			return nil, err
		}
		if err := a.store.DB().QueryRow(`SELECT identity_secret FROM server_identity WHERE singleton=1`).Scan(&secret); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("invalid server identity secret")
	}
	material := append([]byte("locallink:idempotency:v1:"), decoded...)
	sum := sha256.Sum256(material)
	return sum[:], nil
}

func encryptIdempotencyBody(key, plaintext []byte) (nonce, ciphertext string, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonceBytes := make([]byte, gcm.NonceSize())
	if err := encryptRandomNonce(nonceBytes); err != nil {
		return "", "", err
	}
	ciphertextBytes := gcm.Seal(nil, nonceBytes, plaintext, []byte("locallink-idempotency-v1"))
	return base64.RawURLEncoding.EncodeToString(nonceBytes), base64.RawURLEncoding.EncodeToString(ciphertextBytes), nil
}

func encryptRandomNonce(dst []byte) error {
	_, err := rand.Read(dst)
	return err
}

func decryptIdempotencyBody(key []byte, nonceText, ciphertextText string) ([]byte, error) {
	nonce, err := base64.RawURLEncoding.DecodeString(nonceText)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(ciphertextText)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, []byte("locallink-idempotency-v1"))
}

func (a *API) idempotencyScope(r *http.Request, operation string, body []byte) string {
	identity := strings.TrimSpace(r.Header.Get("Authorization")) + "\x00" + strings.TrimSpace(r.Header.Get("X-Device-ID"))
	if identity == "\x00" {
		identity = sha256HexBytes(body)
	}
	return operation + ":" + sha256HexBytes([]byte(identity))
}

func (a *API) WithIdempotency(operation string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !validIdempotencyKey(key) {
			writeError(w, http.StatusBadRequest, "invalid Idempotency-Key")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxIdempotencyBodyStore+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		if len(body) > maxIdempotencyBodyStore {
			writeError(w, http.StatusRequestEntityTooLarge, "idempotent request body is too large")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		scope := a.idempotencyScope(r, operation, body)
		requestHash := sha256HexBytes(body)

		if replay, status, responseBody := a.lookupIdempotency(scope, key, requestHash); replay {
			w.Header().Set("Idempotency-Replayed", "true")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(responseBody)
			return
		}
		claimed, err := a.claimIdempotency(scope, key, requestHash)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to initialize idempotent operation")
			return
		}
		if !claimed {
			writeError(w, http.StatusConflict, "idempotent operation is already in progress")
			return
		}

		capture := newIdempotencyWriter()
		next.ServeHTTP(capture, r)
		for name, values := range capture.header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(capture.status)
		_, _ = w.Write(capture.body.Bytes())

		if capture.status >= 200 && capture.status < 300 && len(capture.body.Bytes()) <= maxIdempotencyBodyStore {
			if err := a.finishIdempotency(scope, key, requestHash, capture.status, capture.body.Bytes()); err != nil {
				return
			}
		} else {
			_ = a.deleteIdempotency(scope, key, requestHash)
		}
	})
}

func (a *API) lookupIdempotency(scope, key, requestHash string) (bool, int, []byte) {
	var storedHash, nonceText, ciphertextText, status string
	var code int
	var createdAt string
	err := a.store.DB().QueryRow(`SELECT request_hash,status_code,status,created_at,response_nonce,response_ciphertext FROM idempotency_keys WHERE scope=? AND key_hash=?`, scope, tokenHash(key)).Scan(&storedHash, &code, &status, &createdAt, &nonceText, &ciphertextText)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil || status != "completed" || storedHash != requestHash {
		if err == nil && storedHash != requestHash {
			return true, http.StatusConflict, []byte(`{"error":"Idempotency-Key was already used with a different request"}`)
		}
		return false, 0, nil
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil || time.Now().UTC().After(created.Add(idempotencyTTL)) {
		_ = a.deleteIdempotency(scope, key, requestHash)
		return false, 0, nil
	}
	encKey, err := a.idempotencyEncryptionKey()
	if err != nil {
		return false, 0, nil
	}
	body, err := decryptIdempotencyBody(encKey, nonceText, ciphertextText)
	if err != nil {
		return false, 0, nil
	}
	return true, code, body
}

func (a *API) claimIdempotency(scope, key, requestHash string) (bool, error) {
	keyHash := tokenHash(key)
	now := time.Now().UTC()
	expires := now.Add(idempotencyInFlightTTL).Format(time.RFC3339Nano)
	result, err := a.store.DB().Exec(`INSERT OR IGNORE INTO idempotency_keys(scope,key_hash,request_hash,status_code,status,created_at,updated_at,expires_at,response_nonce,response_ciphertext) VALUES(?,?,?,0,'in_progress',?,?,?,NULL,NULL)`, scope, keyHash, requestHash, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), expires)
	if err != nil {
		return false, err
	}
	if inserted, _ := result.RowsAffected(); inserted == 1 {
		return true, nil
	}

	var storedHash, status, expiresAt string
	if err := a.store.DB().QueryRow(`SELECT request_hash,status,expires_at FROM idempotency_keys WHERE scope=? AND key_hash=?`, scope, keyHash).Scan(&storedHash, &status, &expiresAt); err != nil {
		return false, err
	}
	if storedHash != requestHash {
		return false, nil
	}
	if status == "completed" {
		return false, nil
	}
	parsedExpiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err == nil && time.Now().UTC().After(parsedExpiry) {
		result, err := a.store.DB().Exec(`UPDATE idempotency_keys SET status='in_progress',status_code=0,created_at=?,updated_at=?,expires_at=?,response_nonce=NULL,response_ciphertext=NULL WHERE scope=? AND key_hash=? AND request_hash=? AND status='in_progress' AND expires_at<=?`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), expires, scope, keyHash, requestHash, now.Format(time.RFC3339Nano))
		if err != nil {
			return false, err
		}
		if n, _ := result.RowsAffected(); n == 1 {
			return true, nil
		}
	}
	return false, nil
}

func (a *API) finishIdempotency(scope, key, requestHash string, statusCode int, body []byte) error {
	encKey, err := a.idempotencyEncryptionKey()
	if err != nil {
		return err
	}
	nonceText, ciphertextText, err := encryptIdempotencyBody(encKey, body)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := a.store.DB().Exec(`UPDATE idempotency_keys SET status_code=?,status='completed',updated_at=?,expires_at=?,response_nonce=?,response_ciphertext=? WHERE scope=? AND key_hash=? AND request_hash=?`, statusCode, now, time.Now().UTC().Add(idempotencyTTL).Format(time.RFC3339Nano), nonceText, ciphertextText, scope, tokenHash(key), requestHash)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("idempotency completion lost ownership")
	}
	return nil
}

func (a *API) deleteIdempotency(scope, key, requestHash string) error {
	_, err := a.store.DB().Exec(`DELETE FROM idempotency_keys WHERE scope=? AND key_hash=? AND request_hash=?`, scope, tokenHash(key), requestHash)
	return err
}
