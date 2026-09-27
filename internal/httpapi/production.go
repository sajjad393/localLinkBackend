package httpapi

import (
	"database/sql"
	"net/http"
	"os"
	"strings"
	"time"
)

// CleanupProductionArtifacts removes expired authentication/recovery artifacts and
// bounds audit-table growth. It is intentionally conservative: business data such
// as messages, calls, groups and files is never deleted by this routine.
func (a *API) CleanupProductionArtifacts() error {
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	_, err := a.store.DB().Exec(`
DELETE FROM auth_sessions WHERE expires_at <= ? OR revoked_at IS NOT NULL AND revoked_at <= ?;
DELETE FROM account_transfer_sessions WHERE expires_at <= ? OR consumed_at IS NOT NULL AND consumed_at <= ?;
DELETE FROM account_recovery_requests WHERE (status IN ('pending','approved','rejected','expired') AND expires_at <= ?) OR (status='completed' AND completed_at IS NOT NULL AND completed_at <= ?);
DELETE FROM idempotency_keys WHERE expires_at <= ?;
DELETE FROM security_audit WHERE created_at <= ?;
DELETE FROM admin_audit WHERE created_at <= ?;`,
		nowText, now.Add(-30*24*time.Hour).Format(time.RFC3339Nano),
		nowText, now.Add(-24*time.Hour).Format(time.RFC3339Nano),
		nowText, now.Add(-30*24*time.Hour).Format(time.RFC3339Nano),
		now.Add(-90*24*time.Hour).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		now.Add(-180*24*time.Hour).Format(time.RFC3339Nano))
	return err
}

func (a *API) DatabaseIntegrity(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	report, err := a.store.IntegrityReport()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database integrity check failed")
		return
	}
	var schemaVersion sql.NullInt64
	_ = a.store.DB().QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&schemaVersion)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                 report.IntegrityCheck == "ok" && len(report.ForeignKeyErrors) == 0,
		"integrity_check":    report.IntegrityCheck,
		"foreign_key_errors": report.ForeignKeyErrors,
		"schema_version":     schemaVersion.Int64,
	})
}

func (a *API) ProductionConfig(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	quota := a.deviceStorageQuota()
	maxFile := a.maxFileSize
	writeJSON(w, http.StatusOK, map[string]any{
		"max_file_bytes":               maxFile,
		"max_storage_per_device_bytes": quota,
		"tls_required":                 strings.EqualFold(strings.TrimSpace(os.Getenv("LOCALLINK_REQUIRE_TLS")), "true"),
	})
}
