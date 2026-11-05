package httpapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bundle 212: trust without internet.
//
// The server owns one Ed25519 signing key (kept in admin_settings so a
// redeploy with the same database keeps it). Phones save its public key the
// first time they reach the server. An owner admin signed in on a phone gets
// an "offline key certificate": the server signs that phone's own signing key.
// Blocks the admin makes offline are signed with that phone key, so any phone
// that knows the server key can check them with no internet.
//
// A certificate ends when that admin sign-in ends (logout, a revoke from the
// Sessions list, or a password reset). The server then signs a small
// "cert_ended" note that phones pass on the same way as blocks.

const serverSigningKeySetting = "server_signing_key"

// signedBlob is a signed JSON body. The signature covers the exact UTF-8
// bytes of Body, so no canonical JSON is needed on either side.
type signedBlob struct {
	Body string `json:"body"`
	Sig  string `json:"sig"`
}

type adminCertBody struct {
	V          int    `json:"v"`
	Kind       string `json:"kind"`
	CertID     string `json:"cert_id"`
	AdminID    string `json:"admin_id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	DeviceID   string `json:"device_id"`
	SigningKey string `json:"signing_key"`
	SessionID  string `json:"session_id"`
	IssuedAt   string `json:"issued_at"`
}

type certEndedBody struct {
	V      int    `json:"v"`
	Kind   string `json:"kind"`
	CertID string `json:"cert_id"`
	At     string `json:"at"`
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func unb64url(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(s), "="))
}

// serverSigningKey loads the server key, creating it once.
func (a *API) serverSigningKey() (ed25519.PrivateKey, error) {
	load := func() (ed25519.PrivateKey, error) {
		var v string
		err := a.store.DB().QueryRow(`SELECT value FROM admin_settings WHERE key=?`, serverSigningKeySetting).Scan(&v)
		if err != nil {
			return nil, err
		}
		seed, err := unb64url(v)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, errors.New("server signing key is damaged")
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	if key, err := load(); err == nil {
		return key, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// INSERT OR IGNORE: if two requests race, the first key wins and both load it.
	if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO admin_settings(key,value,updated_at) VALUES(?,?,?)`, serverSigningKeySetting, b64url(seed), now); err != nil {
		return nil, err
	}
	return load()
}

func serverKeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return b64url(sum[:])[:16]
}

func (a *API) serverSign(body string) (signedBlob, error) {
	key, err := a.serverSigningKey()
	if err != nil {
		return signedBlob{}, err
	}
	return signedBlob{Body: body, Sig: b64url(ed25519.Sign(key, []byte(body)))}, nil
}

func (a *API) serverVerify(blob signedBlob) bool {
	key, err := a.serverSigningKey()
	if err != nil {
		return false
	}
	sig, err := unb64url(blob.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(key.Public().(ed25519.PublicKey), []byte(blob.Body), sig)
}

func verifyWithKey(publicKeyB64 string, blob signedBlob) bool {
	pub, err := unb64url(publicKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := unb64url(blob.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), []byte(blob.Body), sig)
}

// Trust returns the server public key. No auth: it is public by design.
func (a *API) Trust(w http.ResponseWriter, r *http.Request) {
	key, err := a.serverSigningKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "trust key unavailable")
		return
	}
	pub := key.Public().(ed25519.PublicKey)
	out := map[string]any{"server_key": b64url(pub), "key_id": serverKeyID(pub)}
	// Bundle 222: the server box key for join boxes, signed (phones hand it on).
	if note, joinKey, err := a.joinNote222(); err == nil {
		out["join_key"], out["join_note"] = joinKey, note
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminOfflineCert signs this phone's signing key for the signed-in owner.
func (a *API) AdminOfflineCert(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_users")
	if !ok {
		return
	}
	var in struct {
		DeviceID   string `json:"device_id"`
		SigningKey string `json:"signing_key"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.DeviceID = strings.TrimSpace(in.DeviceID)
	in.SigningKey = strings.TrimSpace(in.SigningKey)
	if pub, err := unb64url(in.SigningKey); err != nil || len(pub) != ed25519.PublicKeySize || in.DeviceID == "" || len(in.DeviceID) > 200 {
		writeError(w, http.StatusBadRequest, "invalid device or signing key")
		return
	}
	sessionID := a.adminSessionIDFor(r)
	if sessionID == "" {
		writeError(w, http.StatusUnauthorized, "admin session required")
		return
	}
	// Same sign-in + same phone key: give the same certificate back.
	var body, sig string
	err := a.store.DB().QueryRow(`SELECT body,sig FROM admin_offline_certs WHERE session_id=? AND signing_key=? AND ended_at IS NULL ORDER BY issued_at DESC LIMIT 1`, sessionID, in.SigningKey).Scan(&body, &sig)
	if err == nil {
		a.writeCert(w, signedBlob{Body: body, Sig: sig})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cert := adminCertBody{V: 1, Kind: "admin_cert", CertID: uuid.NewString(), AdminID: admin.ID, Username: admin.Username,
		Role: admin.Role, DeviceID: in.DeviceID, SigningKey: in.SigningKey, SessionID: sessionID, IssuedAt: now}
	raw, _ := json.Marshal(cert)
	blob, err := a.serverSign(string(raw))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign certificate")
		return
	}
	if _, err := a.store.DB().Exec(`INSERT INTO admin_offline_certs(cert_id,admin_id,session_id,device_id,signing_key,body,sig,issued_at) VALUES(?,?,?,?,?,?,?,?)`,
		cert.CertID, admin.ID, sessionID, in.DeviceID, in.SigningKey, blob.Body, blob.Sig, now); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save certificate")
		return
	}
	a.adminAudit(admin.ID, "admin_offline_cert", in.DeviceID, "cert="+cert.CertID)
	a.writeCert(w, blob)
}

func (a *API) writeCert(w http.ResponseWriter, blob signedBlob) {
	key, err := a.serverSigningKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "trust key unavailable")
		return
	}
	pub := key.Public().(ed25519.PublicKey)
	writeJSON(w, http.StatusOK, map[string]any{"cert": blob, "server_key": b64url(pub), "key_id": serverKeyID(pub)})
}

// adminSessionIDFor returns the admin session id behind the bearer token.
// The test/dev static token gets a fixed id.
func (a *API) adminSessionIDFor(r *http.Request) string {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token != "" {
		var id string
		if err := a.store.DB().QueryRow(`SELECT id FROM admin_sessions WHERE token_hash=? AND revoked_at IS NULL`, tokenHash(token)).Scan(&id); err == nil {
			return id
		}
	}
	if strings.TrimSpace(r.Header.Get("X-Admin-Token")) != "" {
		return "static-admin"
	}
	return ""
}

// endCerts ends every live certificate matching the WHERE clause and signs
// one "cert_ended" note per certificate. Best effort: a failure never blocks
// the logout / revoke / reset itself.
func (a *API) endCerts(where string, arg string) {
	rows, err := a.store.DB().Query(`SELECT cert_id FROM admin_offline_certs WHERE ended_at IS NULL AND `+where, arg)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, id := range ids {
		raw, _ := json.Marshal(certEndedBody{V: 1, Kind: "cert_ended", CertID: id, At: now})
		blob, err := a.serverSign(string(raw))
		if err != nil {
			continue
		}
		_, _ = a.store.DB().Exec(`UPDATE admin_offline_certs SET ended_at=? WHERE cert_id=? AND ended_at IS NULL`, now, id)
		_, _ = a.store.DB().Exec(`INSERT OR IGNORE INTO admin_cert_ended(cert_id,body,sig,at) VALUES(?,?,?,?)`, id, blob.Body, blob.Sig, now)
	}
}

func (a *API) endCertsForSessionID(sessionID string) {
	if strings.TrimSpace(sessionID) != "" {
		a.endCerts("session_id=?", sessionID)
	}
}

func (a *API) endCertsForTokenHash(hash string) {
	var id string
	if err := a.store.DB().QueryRow(`SELECT id FROM admin_sessions WHERE token_hash=?`, hash).Scan(&id); err == nil {
		a.endCertsForSessionID(id)
	}
}

func (a *API) endCertsForAdmin(adminID string) {
	if strings.TrimSpace(adminID) != "" {
		a.endCerts("admin_id=?", adminID)
	}
}

func (a *API) certEndedNotes() []signedBlob {
	rows, err := a.store.DB().Query(`SELECT body,sig FROM admin_cert_ended ORDER BY at DESC LIMIT 200`)
	if err != nil {
		return []signedBlob{}
	}
	defer rows.Close()
	out := []signedBlob{}
	for rows.Next() {
		var b signedBlob
		if rows.Scan(&b.Body, &b.Sig) == nil {
			out = append(out, b)
		}
	}
	return out
}
