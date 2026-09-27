package httpapi

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	maxLoginAttemptsPerMinute     = 10
	maxAdminAttemptsPerMinute     = 30
	maxSensitiveAttemptsPerMinute = 12
	loginFailureThreshold         = 8
	loginLockDuration             = 15 * time.Minute
	securityAuditRetention        = 90 * 24 * time.Hour
	authSessionRetention          = 7 * 24 * time.Hour
	transferRetention             = 24 * time.Hour
	recoveryRetention             = 30 * 24 * time.Hour
)

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
}

// allowSecurityAction provides a small in-memory abuse barrier for account,
// recovery, transfer, and admin endpoints. It intentionally complements the
// database-backed one-time credentials rather than replacing them.
func (a *API) allowSecurityAction(scope, key string, max int) bool {
	if strings.TrimSpace(key) == "" || max < 1 {
		return false
	}
	now := time.Now()
	composite := scope + "|" + key
	a.securityMu.Lock()
	defer a.securityMu.Unlock()
	state := a.securityRates[composite]
	if state.started.IsZero() || now.Sub(state.started) >= time.Minute {
		a.securityRates[composite] = messageRate{started: now, count: 1}
		if len(a.securityRates) > 20000 {
			for k, v := range a.securityRates {
				if now.Sub(v.started) >= time.Minute {
					delete(a.securityRates, k)
				}
			}
		}
		return true
	}
	if state.count >= max {
		return false
	}
	state.count++
	a.securityRates[composite] = state
	return true
}

// allowPersistentSecurityAction adds a DB-backed rate limiter for sensitive
// operations. Unlike the in-memory barrier, it survives process restarts and
// does not depend solely on source-address rotation. Keys are hashed before
// persistence so usernames/request identifiers never appear directly in the table.
func (a *API) allowPersistentSecurityAction(scope, key string, max int, window time.Duration) bool {
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(key) == "" || max < 1 || window <= 0 {
		return false
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	windowStart := now.Add(-window)
	rateKey := rateLimitKeyHash(scope, key)

	// Keys are hashed before persistence so usernames/request identifiers never
	// appear directly in the limiter table.
	tx, err := a.store.DB().Begin()
	if err != nil {
		return false
	}
	defer tx.Rollback()

	// Keep stale limiter buckets bounded. A short hard ceiling avoids turning
	// this table into an unbounded abuse log even when attackers vary keys.
	if _, err := tx.Exec(`DELETE FROM security_rate_limits WHERE updated_at < ?`, now.Add(-2*time.Hour).Format(time.RFC3339Nano)); err != nil {
		return false
	}

	var startedText string
	var count int
	err = tx.QueryRow(`SELECT window_started_at,count FROM security_rate_limits WHERE scope=? AND rate_key=?`, scope, rateKey).Scan(&startedText, &count)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.Exec(`INSERT INTO security_rate_limits(scope,rate_key,window_started_at,count,updated_at) VALUES(?,?,?,?,?)`, scope, rateKey, nowText, 1, nowText); err != nil {
			return false
		}
		return tx.Commit() == nil
	}
	if err != nil {
		return false
	}
	started, parseErr := time.Parse(time.RFC3339Nano, startedText)
	if parseErr != nil || started.Before(windowStart) {
		if _, err = tx.Exec(`UPDATE security_rate_limits SET window_started_at=?,count=1,updated_at=? WHERE scope=? AND rate_key=?`, nowText, nowText, scope, rateKey); err != nil {
			return false
		}
		return tx.Commit() == nil
	}
	if count >= max {
		return false
	}
	if _, err = tx.Exec(`UPDATE security_rate_limits SET count=count+1,updated_at=? WHERE scope=? AND rate_key=?`, nowText, scope, rateKey); err != nil {
		return false
	}
	return tx.Commit() == nil
}

func rateLimitKeyHash(scope, key string) string {
	sum := sha256.Sum256([]byte("locallink-rate-v1|" + scope + "|" + key))
	return hex.EncodeToString(sum[:])
}

// allowDeviceAction combines a fast in-memory device barrier with a persistent
// device/account barrier. The persistent keys are hashed and therefore do not
// expose device IDs or account IDs in the limiter table.
func (a *API) allowDeviceAction(scope, deviceID string, perDevice, perAccount int) bool {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || perDevice < 1 || perAccount < 1 {
		return false
	}
	if !a.allowSecurityAction(scope+"-device", deviceID, perDevice) {
		return false
	}
	if !a.allowPersistentSecurityAction(scope+"-device", deviceID, perDevice, time.Minute) {
		return false
	}
	var accountID string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(user_id,'') FROM devices WHERE id=?`, deviceID).Scan(&accountID); err != nil || strings.TrimSpace(accountID) == "" {
		return true
	}
	return a.allowPersistentSecurityAction(scope+"-account", accountID, perAccount, time.Minute)
}

func authUsernameRateKey(r *http.Request, username string) string {
	return registrationKey(r) + "|" + strings.ToLower(strings.TrimSpace(username))
}

func (a *API) recordLoginFailure(userID string) (lockedUntil time.Time) {
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	tx, err := a.store.DB().Begin()
	if err != nil {
		a.auditSecurity("login_failed", userID, "", rSourceUnknown(), "database error while recording failed login")
		return time.Time{}
	}
	defer tx.Rollback()

	var count int
	var lastFailed string
	_ = tx.QueryRow(`SELECT COALESCE(failed_login_count,0),COALESCE(last_failed_login_at,'') FROM users WHERE id=?`, userID).Scan(&count, &lastFailed)
	if parsed, err := time.Parse(time.RFC3339Nano, lastFailed); err == nil && now.Sub(parsed) > loginLockDuration {
		count = 0
	}
	count++
	if count >= loginFailureThreshold {
		lockedUntil = now.Add(loginLockDuration)
	}
	if lockedUntil.IsZero() {
		_, _ = tx.Exec(`UPDATE users SET failed_login_count=?,last_failed_login_at=?,updated_at=? WHERE id=?`, count, nowText, nowText, userID)
	} else {
		_, _ = tx.Exec(`UPDATE users SET failed_login_count=?,last_failed_login_at=?,locked_until=?,updated_at=? WHERE id=?`, count, nowText, lockedUntil.Format(time.RFC3339Nano), nowText, userID)
	}
	_ = tx.Commit()
	return lockedUntil
}

func (a *API) resetLoginFailures(userID string) {
	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = a.store.DB().Exec(`UPDATE users SET failed_login_count=0,last_failed_login_at=NULL,locked_until=NULL,updated_at=? WHERE id=?`, nowText, userID)
}

func (a *API) loginLockedUntil(userID string) time.Time {
	var raw sql.NullString
	if err := a.store.DB().QueryRow(`SELECT locked_until FROM users WHERE id=?`, userID).Scan(&raw); err != nil || !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil || !time.Now().UTC().Before(parsed) {
		return time.Time{}
	}
	return parsed
}

func sourceHash(source string) string {
	sum := sha256.Sum256([]byte("locallink-audit-v1|" + source))
	return hex.EncodeToString(sum[:])
}

func rSourceUnknown() string { return "unknown" }

func (a *API) auditSecurity(action, userID, deviceID, source, detail string) {
	_, _ = a.store.DB().Exec(`INSERT INTO security_audit(action,user_id,device_id,source_hash,detail,created_at) VALUES(?,?,?,?,?,?)`, action, userID, deviceID, sourceHash(source), detail, time.Now().UTC().Format(time.RFC3339Nano))
}

// CleanupSecurityArtifacts removes expired bearer credentials and security
// events while preserving recent audit history needed for incident review.
func (a *API) CleanupSecurityArtifacts() error {
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	if _, err := a.store.DB().Exec(`UPDATE account_recovery_requests SET status='expired',completed_at=COALESCE(completed_at,?) WHERE status='pending' AND expires_at<=?`, nowText, nowText); err != nil {
		return err
	}
	if _, err := a.store.DB().Exec(`UPDATE account_recovery_requests SET recovery_credential_hash=NULL,recovery_expires_at=NULL WHERE status='approved' AND recovery_expires_at IS NOT NULL AND recovery_expires_at<=?`, nowText); err != nil {
		return err
	}
	if _, err := a.store.DB().Exec(`DELETE FROM auth_sessions WHERE expires_at<=? OR (revoked_at IS NOT NULL AND revoked_at<=?)`, nowText, now.Add(-authSessionRetention).Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := a.store.DB().Exec(`DELETE FROM admin_sessions WHERE expires_at<=? OR (revoked_at IS NOT NULL AND revoked_at<=?)`, nowText, now.Add(-authSessionRetention).Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := a.store.DB().Exec(`DELETE FROM account_transfer_sessions WHERE expires_at<=? OR (consumed_at IS NOT NULL AND consumed_at<=?)`, now.Add(-transferRetention).Format(time.RFC3339Nano), now.Add(-transferRetention).Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := a.store.DB().Exec(`DELETE FROM account_recovery_requests WHERE (status IN ('completed','rejected','expired') AND created_at<=?)`, now.Add(-recoveryRetention).Format(time.RFC3339Nano)); err != nil {
		return err
	}
	_, err := a.store.DB().Exec(`DELETE FROM security_audit WHERE created_at<=?`, now.Add(-securityAuditRetention).Format(time.RFC3339Nano))
	return err
}
