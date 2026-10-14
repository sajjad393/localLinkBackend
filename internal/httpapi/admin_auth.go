package httpapi

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const adminSessionTTL = 30 * time.Minute

type AdminAccount struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type AdminLoginResponse struct {
	Admin     AdminAccount `json:"admin"`
	Token     string       `json:"token"`
	ExpiresAt string       `json:"expires_at"`
}

func (a *API) ensureBootstrapAdmin() error {
	var count int
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_accounts WHERE status='active'`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	username := strings.TrimSpace(os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME"))
	password := os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD")
	if username == "" || len(password) < 12 {
		return errors.New("admin bootstrap credentials are required on first deployment")
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hash := derivePasswordKey(password, salt, passwordIterations)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := a.store.DB().Exec(`INSERT OR IGNORE INTO admin_accounts(id,username,password_salt,password_hash,password_iterations,role,status,created_at,updated_at) VALUES(?,?,?,?,?,'owner','active',?,?)`, uuid.NewString(), strings.ToLower(username), base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(hash), passwordIterations, now, now)
	return err
}

func (a *API) ResetAdminCredentials() error {
	username := strings.ToLower(strings.TrimSpace(os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME")))
	password := os.Getenv("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD")

	if username == "" {
		return errors.New("LOCALLINK_ADMIN_BOOTSTRAP_USERNAME is required")
	}
	if len(password) < 12 {
		return errors.New("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD must be at least 12 characters")
	}
	if hasControlChars(password) {
		return errors.New("LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD contains invalid control characters")
	}

	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return err
	}

	hash := derivePasswordKey(password, salt, passwordIterations)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := a.store.DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var adminID string

	err = tx.QueryRow(
		`SELECT id FROM admin_accounts WHERE username=? LIMIT 1`,
		username,
	).Scan(&adminID)

	if err == sql.ErrNoRows {
		adminID = uuid.NewString()

		_, err = tx.Exec(`
			INSERT INTO admin_accounts(
				id,
				username,
				password_salt,
				password_hash,
				password_iterations,
				role,
				status,
				created_at,
				updated_at
			)
			VALUES(?,?,?,?,?,'owner','active',?,?)
		`,
			adminID,
			username,
			base64.RawURLEncoding.EncodeToString(salt),
			base64.RawURLEncoding.EncodeToString(hash),
			passwordIterations,
			now,
			now,
		)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		_, err = tx.Exec(`
			UPDATE admin_accounts
			SET
				password_salt=?,
				password_hash=?,
				password_iterations=?,
				role='owner',
				status='active',
				updated_at=?
			WHERE id=?
		`,
			base64.RawURLEncoding.EncodeToString(salt),
			base64.RawURLEncoding.EncodeToString(hash),
			passwordIterations,
			now,
			adminID,
		)
		if err != nil {
			return err
		}
	}

	// Revoke existing admin sessions after a credential reset.
	if _, err := tx.Exec(
		`UPDATE admin_sessions SET revoked_at=? WHERE admin_id=? AND revoked_at IS NULL`,
		now,
		adminID,
	); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (a *API) AdminLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &in, 12<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if !a.allowSecurityAction("admin-login-ip", registrationKey(r), maxAdminAttemptsPerMinute) ||
		!a.allowSecurityAction("admin-login-user", authUsernameRateKey(r, username), maxAdminAttemptsPerMinute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "admin login rate limit exceeded")
		return
	}
	if username == "" || len(in.Password) < 8 || len(in.Password) > maxPasswordLen || hasControlChars(in.Password) {
		writeError(w, http.StatusBadRequest, "invalid admin credentials")
		return
	}
	var admin AdminAccount
	var saltB64, hashB64 string
	var iterations int
	if err := a.store.DB().QueryRow(`SELECT id,username,role,status,created_at,password_salt,password_hash,password_iterations FROM admin_accounts WHERE username=?`, username).Scan(&admin.ID, &admin.Username, &admin.Role, &admin.Status, &admin.CreatedAt, &saltB64, &hashB64, &iterations); err != nil {
		_ = derivePasswordKey(in.Password, []byte("locallink-admin-dummy-salt-v1"), passwordIterations)
		writeError(w, http.StatusUnauthorized, "invalid admin credentials")
		return
	}
	salt, err1 := base64.RawURLEncoding.DecodeString(saltB64)
	expected, err2 := base64.RawURLEncoding.DecodeString(hashB64)
	if err1 != nil || err2 != nil || len(expected) != passwordKeyLength || !hmacEqualBytes(derivePasswordKey(in.Password, salt, iterations), expected) || admin.Status != "active" {
		writeError(w, http.StatusUnauthorized, "invalid admin credentials")
		return
	}
	token, err := newToken()
	if err != nil {
		writeError(w, 500, "failed to create admin session")
		return
	}
	now := time.Now().UTC()
	expires := now.Add(adminSessionTTL)
	sessionID := uuid.NewString()
	if _, err := a.store.DB().Exec(`INSERT INTO admin_sessions(id,admin_id,token_hash,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?)`, sessionID, admin.ID, tokenHash(token), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano)); err != nil {
		writeError(w, 500, "failed to create admin session")
		return
	}
	_, _ = a.store.DB().Exec(`UPDATE admin_accounts SET last_login_at=?,updated_at=? WHERE id=?`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), admin.ID)
	a.adminAudit(admin.ID, "admin_login", "", "authenticated")
	writeJSON(w, http.StatusOK, AdminLoginResponse{Admin: admin, Token: token, ExpiresAt: expires.Format(time.RFC3339Nano)})
}

func (a *API) AdminLogout(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "admin session required")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tokenHashValue := tokenHash(token)
	admin, authenticated := a.authorizeAdminSession(r)
	res, err := a.store.DB().Exec(`UPDATE admin_sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, now, tokenHashValue)
	if err != nil {
		writeError(w, 500, "failed to revoke admin session")
		return
	}
	if authenticated && resAffected(res) > 0 {
		a.adminAudit(admin.ID, "admin_logout", "", "authenticated session revoked")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": resAffected(res)})
}

func resAffected(r sql.Result) int64 { n, _ := r.RowsAffected(); return n }

func hmacEqualBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func (a *API) authorizeAdminSession(r *http.Request) (AdminAccount, bool) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" {
		return AdminAccount{}, false
	}
	var out AdminAccount
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err := a.store.DB().QueryRow(`SELECT aa.id,aa.username,aa.role,aa.status,aa.created_at FROM admin_sessions s JOIN admin_accounts aa ON aa.id=s.admin_id WHERE s.token_hash=? AND s.revoked_at IS NULL AND s.expires_at>? AND aa.status='active'`, tokenHash(token), now).Scan(&out.ID, &out.Username, &out.Role, &out.Status, &out.CreatedAt)
	if err != nil {
		return AdminAccount{}, false
	}
	_, _ = a.store.DB().Exec(`UPDATE admin_sessions SET last_seen_at=? WHERE token_hash=? AND revoked_at IS NULL`, now, tokenHash(token))
	return out, true
}

func adminPermission(role, permission string) bool {
	switch role {
	case "owner":
		return true
	case "operator":
		return permission == "read" || permission == "manage_devices" || permission == "manage_recovery" || permission == "manage_network_policy"
	case "security":
		return permission == "read" || permission == "manage_recovery"
	case "auditor":
		return permission == "read"
	default:
		return false
	}
}

func (a *API) requireAdminPermission(w http.ResponseWriter, r *http.Request, permission string) (AdminAccount, bool) {
	// Credentialled requests get the console polling budget; anonymous probes
	// keep the tighter brute-force cap. The two counters are separate buckets so
	// neither ceiling is consumed by the other's traffic — both remain per
	// source address per minute.
	bucket, limit := "admin-ip", maxAdminAttemptsPerMinute
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" || strings.TrimSpace(r.Header.Get("X-Admin-Token")) != "" {
		bucket, limit = "admin-authed-ip", maxAdminAuthedRequestsPerMin
	}
	if !a.allowSecurityAction(bucket, registrationKey(r), limit) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "admin rate limit exceeded")
		return AdminAccount{}, false
	}
	admin, ok := a.authorizeAdminSession(r)
	if !ok {
		// Static credentials are accepted only for explicit local test/dev mode.
		env := strings.ToLower(strings.TrimSpace(getenv("LOCALLINK_ENV", "production")))
		if adminStaticTokenAllowed(env) {
			expected := strings.TrimSpace(os.Getenv("LOCALLINK_ADMIN_TOKEN"))
			provided := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
			if expected != "" && secureTokenEqual(expected, provided) {
				return AdminAccount{ID: "legacy-static-admin", Username: "legacy-static-admin", Role: "owner", Status: "active"}, true
			}
		}
		writeError(w, 401, "invalid admin credentials")
		return AdminAccount{}, false
	}
	if !adminPermission(admin.Role, permission) {
		a.adminAudit(admin.ID, "admin_permission_denied", "", permission)
		writeError(w, 403, "admin permission denied")
		return AdminAccount{}, false
	}
	return admin, true
}

func adminStaticTokenAllowed(env string) bool {
	env = strings.ToLower(strings.TrimSpace(env))
	return env == "test" || env == "development"
}

func (a *API) adminAudit(adminID, action, target, detail string) {
	_, _ = a.store.DB().Exec(`INSERT INTO admin_audit(admin_id,action,target_id,detail,created_at) VALUES(?,?,?,?,?)`, adminID, action, target, detail, time.Now().UTC().Format(time.RFC3339Nano))
}
