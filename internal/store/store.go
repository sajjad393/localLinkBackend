package store

import (
	"context"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(10000)&_pragma=synchronous(FULL)&_pragma=wal_autocheckpoint(1000)")
	if err != nil {
		return nil, err
	}
	// database/sql opens lazily. Ping first so a brand-new SQLite file exists
	// before we enforce its filesystem permissions.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if path != ":memory:" && path != "" {
		if err := os.Chmod(path, 0600); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Ping() error  { return s.db.Ping() }
func (s *Store) DB() *sql.DB  { return s.db }

// WithTxRetry executes fn inside a transaction and retries transactions that
// fail with SQLite busy/locked errors before commit. Commit errors are returned
// directly because a commit failure can be ambiguous and must not be replayed.
func (s *Store) WithTxRetry(ctx context.Context, fn func(*sql.Tx) error) error {
	if fn == nil {
		return fmt.Errorf("transaction callback is nil")
	}
	const attempts = 5
	for attempt := 0; attempt < attempts; attempt++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			if !isSQLiteBusyError(err) || attempt == attempts-1 {
				return err
			}
			if err := sleepWithContext(ctx, time.Duration(25*(1<<attempt))*time.Millisecond); err != nil {
				return err
			}
			continue
		}

		err = fn(tx)
		if err == nil {
			err = tx.Commit()
			if err == nil {
				return nil
			}
			// A failed commit may have reached SQLite already. Never replay it.
			return err
		}
		_ = tx.Rollback()
		if !isSQLiteBusyError(err) || attempt == attempts-1 {
			return err
		}
		if err := sleepWithContext(ctx, time.Duration(25*(1<<attempt))*time.Millisecond); err != nil {
			return err
		}
	}
	return context.Canceled
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database table is locked") || strings.Contains(msg, "database is busy")
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// IntegrityReport performs a fast SQLite integrity check plus foreign-key
// validation. It is intentionally explicit so callers can decide whether to
// run it at startup, on demand, or from the admin diagnostics endpoint.
type IntegrityReport struct {
	IntegrityCheck   string
	ForeignKeyErrors []string
}

func (s *Store) IntegrityReport() (IntegrityReport, error) {
	var report IntegrityReport
	if err := s.db.QueryRow(`PRAGMA quick_check(1)`).Scan(&report.IntegrityCheck); err != nil {
		return report, err
	}
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		var rowID, parent sql.NullString
		var fkIndex int
		if err := rows.Scan(&table, &rowID, &parent, &fkIndex); err != nil {
			return report, err
		}
		report.ForeignKeyErrors = append(report.ForeignKeyErrors, fmt.Sprintf("%s:%s:%s:%d", table, nullableString(rowID), nullableString(parent), fkIndex))
	}
	if err := rows.Err(); err != nil {
		return report, err
	}
	return report, nil
}

func nullableString(v sql.NullString) string {
	if !v.Valid {
		return "NULL"
	}
	return v.String
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS security_rate_limits (
    scope TEXT NOT NULL,
    rate_key TEXT NOT NULL,
    window_started_at TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(scope, rate_key)
);
CREATE INDEX IF NOT EXISTS idx_security_rate_limits_updated ON security_rate_limits(updated_at);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    scope TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status_code INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'in_progress',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    response_nonce TEXT,
    response_ciphertext TEXT,
    PRIMARY KEY(scope, key_hash)
);
CREATE INDEX IF NOT EXISTS idx_idempotency_expiry ON idempotency_keys(expires_at);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_salt TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    password_iterations INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    failed_login_count INTEGER NOT NULL DEFAULT 0,
    last_failed_login_at TEXT,
    locked_until TEXT
);
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
CREATE INDEX IF NOT EXISTS idx_users_locked_until ON users(locked_until);

CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    platform TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    token_hash TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    revoked_at TEXT,
    identity_public_key TEXT,
    identity_key_version INTEGER NOT NULL DEFAULT 1,
    user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    hardware_fp TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);

CREATE TABLE IF NOT EXISTS device_identity_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    key_version INTEGER NOT NULL,
    public_key TEXT NOT NULL,
    created_at TEXT NOT NULL,
    retired_at TEXT,
    UNIQUE(device_id, key_version),
    UNIQUE(device_id, public_key)
);
CREATE INDEX IF NOT EXISTS idx_device_identity_keys_device ON device_identity_keys(device_id, key_version);

CREATE TABLE IF NOT EXISTS auth_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_device ON auth_sessions(device_id, revoked_at, expires_at);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id, revoked_at, expires_at);

CREATE TABLE IF NOT EXISTS profiles (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL,
    avatar_path TEXT,
    avatar_content_type TEXT,
    avatar_size INTEGER NOT NULL DEFAULT 0,
    avatar_sha256 TEXT NOT NULL DEFAULT '',
    avatar_updated_at TEXT,
    phone_number TEXT NOT NULL DEFAULT '',
    phone_visibility TEXT NOT NULL DEFAULT 'contacts',
    discoverable_by_phone INTEGER NOT NULL DEFAULT 1,
    discoverable_by_name INTEGER NOT NULL DEFAULT 1,
    directory_sync_enabled INTEGER NOT NULL DEFAULT 1,
    profile_version INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS peer_keys (
    device_a TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    device_b TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    shared_key TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(device_a, device_b),
    CHECK(device_a < device_b)
);
CREATE INDEX IF NOT EXISTS idx_peer_keys_b ON peer_keys(device_b);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    sender_id TEXT NOT NULL,
    recipient_id TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'sent',
    delivered_at TEXT,
    server_seq INTEGER
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation
    ON messages(sender_id, recipient_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_messages_recipient
    ON messages(recipient_id, created_at, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_server_seq
    ON messages(server_seq);

CREATE TABLE IF NOT EXISTS groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    owner_id TEXT NOT NULL REFERENCES devices(id),
    created_at TEXT NOT NULL,
    key_version INTEGER NOT NULL DEFAULT 1,
    rekey_required INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS group_members (
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    joined_at TEXT NOT NULL,
    PRIMARY KEY(group_id, device_id)
);
CREATE INDEX IF NOT EXISTS idx_group_members_device ON group_members(device_id);
CREATE TABLE IF NOT EXISTS group_messages (
    id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    sender_id TEXT NOT NULL REFERENCES devices(id),
    body TEXT NOT NULL,
    created_at TEXT NOT NULL,
    server_seq INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_group_messages_seq ON group_messages(group_id, server_seq);
CREATE TABLE IF NOT EXISTS sync_events (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    event_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sync_events_device_seq ON sync_events(device_id, seq);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sync_events_device_event ON sync_events(device_id, event_type, event_id);
CREATE INDEX IF NOT EXISTS idx_group_messages_group ON group_messages(group_id, server_seq);

CREATE TABLE IF NOT EXISTS calls (
    id TEXT PRIMARY KEY,
    caller_id TEXT NOT NULL REFERENCES devices(id),
    callee_id TEXT NOT NULL REFERENCES devices(id),
    status TEXT NOT NULL,
    started_at TEXT NOT NULL,
    answered_at TEXT,
    ended_at TEXT,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    end_reason TEXT NOT NULL DEFAULT '',
    last_activity_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_calls_caller_started ON calls(caller_id, started_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_calls_callee_started ON calls(callee_id, started_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS server_identity (
    singleton INTEGER PRIMARY KEY CHECK(singleton=1),
    server_id TEXT NOT NULL UNIQUE,
    identity_secret TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_accounts (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_salt TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    password_iterations INTEGER NOT NULL,
    role TEXT NOT NULL DEFAULT 'operator',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_login_at TEXT
);
CREATE TABLE IF NOT EXISTS admin_sessions (
    id TEXT PRIMARY KEY,
    admin_id TEXT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_admin ON admin_sessions(admin_id, revoked_at, expires_at);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_token ON admin_sessions(token_hash, revoked_at, expires_at);
CREATE TABLE IF NOT EXISTS admin_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    admin_id TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_admin_audit_created ON admin_audit(created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS security_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    action TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT '',
    device_id TEXT NOT NULL DEFAULT '',
    source_hash TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_security_audit_created ON security_audit(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_security_audit_user ON security_audit(user_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS account_transfer_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    secret_hash TEXT NOT NULL,
    server_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    target_device_id TEXT
);
CREATE INDEX IF NOT EXISTS idx_account_transfer_source ON account_transfer_sessions(source_device_id, consumed_at, expires_at);
CREATE INDEX IF NOT EXISTS idx_account_transfer_user ON account_transfer_sessions(user_id, consumed_at, expires_at);

CREATE TABLE IF NOT EXISTS account_recovery_requests (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_device_id TEXT NOT NULL,
    target_device_name TEXT NOT NULL,
    target_platform TEXT NOT NULL,
    target_identity_public_key TEXT NOT NULL,
    request_secret_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    approved_at TEXT,
    recovery_expires_at TEXT,
    recovery_credential_hash TEXT,
    rejected_at TEXT,
    rejected_reason TEXT NOT NULL DEFAULT '',
    completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_recovery_user_status ON account_recovery_requests(user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_recovery_status_expiry ON account_recovery_requests(status, expires_at);

CREATE TABLE IF NOT EXISTS account_crypto_backups (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    envelope TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    source_device_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_crypto_backups_updated ON account_crypto_backups(updated_at DESC);

CREATE TABLE IF NOT EXISTS files (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    original_name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    thumbnail_path TEXT,
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    is_image INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ready',
    storage_size INTEGER NOT NULL DEFAULT 0,
    thumbnail_storage_size INTEGER NOT NULL DEFAULT 0,
    crypto_version TEXT NOT NULL DEFAULT '',
    crypto_scope TEXT NOT NULL DEFAULT '',
    crypto_key_version INTEGER NOT NULL DEFAULT 0,
    crypto_nonce TEXT,
    crypto_mac TEXT,
    thumbnail_crypto_nonce TEXT,
    thumbnail_crypto_mac TEXT
);
CREATE INDEX IF NOT EXISTS idx_files_owner_hash ON files(owner_id, sha256);

CREATE TABLE IF NOT EXISTS message_attachments (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    file_id TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(message_id, file_id)
);
CREATE INDEX IF NOT EXISTS idx_message_attachments_file ON message_attachments(file_id);

CREATE TABLE IF NOT EXISTS group_message_attachments (
    group_message_id TEXT NOT NULL REFERENCES group_messages(id) ON DELETE CASCADE,
    file_id TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(group_message_id, file_id)
);
CREATE INDEX IF NOT EXISTS idx_group_message_attachments_file ON group_message_attachments(file_id);
`); err != nil {
		return err
	}

	if err := s.ensureColumn("messages", "status", "TEXT NOT NULL DEFAULT 'sent'"); err != nil {
		return err
	}
	if err := s.ensureColumn("messages", "delivered_at", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("messages", "server_seq", "INTEGER"); err != nil {
		return err
	}
	if err := s.ensureColumn("calls", "last_activity_at", "TEXT"); err != nil {
		return err
	}
	if _, err := s.db.Exec("UPDATE calls SET last_activity_at=COALESCE(last_activity_at,started_at) WHERE last_activity_at IS NULL OR last_activity_at=''"); err != nil {
		return err
	}
	if err := s.ensureColumn("devices", "token_hash", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("devices", "status", "TEXT NOT NULL DEFAULT 'active'"); err != nil {
		return err
	}
	if err := s.ensureColumn("devices", "revoked_at", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("devices", "identity_public_key", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("devices", "identity_key_version", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn("devices", "user_id", "TEXT"); err != nil {
		return err
	}
	// Stable hardware fingerprint lets a reinstalled app with a cleared device
	// id re-bind to its existing device row instead of creating a duplicate.
	if err := s.ensureColumn("devices", "hardware_fp", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_devices_hardware_fp ON devices(hardware_fp) WHERE hardware_fp <> ''`); err != nil {
		return err
	}
	// Server-backed contact list so contacts survive app reinstall/login on a
	// fresh device (they were previously local-only and lost on uninstall).
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS account_contacts (
		user_id TEXT NOT NULL,
		contact_user_id TEXT NOT NULL,
		added_at TEXT NOT NULL,
		PRIMARY KEY(user_id, contact_user_id)
	)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL COLLATE NOCASE UNIQUE,
		password_salt TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		password_iterations INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	); CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS auth_sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL,
		created_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		revoked_at TEXT
	); CREATE INDEX IF NOT EXISTS idx_auth_sessions_device ON auth_sessions(device_id, revoked_at, expires_at); CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id, revoked_at, expires_at);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS profiles (
		user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		display_name TEXT NOT NULL,
		avatar_path TEXT,
		avatar_content_type TEXT,
		avatar_size INTEGER NOT NULL DEFAULT 0,
		avatar_sha256 TEXT NOT NULL DEFAULT '',
		avatar_updated_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		return err
	}

	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS account_recovery_requests (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		target_device_id TEXT NOT NULL,
		target_device_name TEXT NOT NULL,
		target_platform TEXT NOT NULL,
		target_identity_public_key TEXT NOT NULL,
		request_secret_hash TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		approved_at TEXT,
		recovery_expires_at TEXT,
		recovery_credential_hash TEXT,
		rejected_at TEXT,
		rejected_reason TEXT NOT NULL DEFAULT '',
		completed_at TEXT
	); CREATE INDEX IF NOT EXISTS idx_recovery_user_status ON account_recovery_requests(user_id, status, created_at DESC); CREATE INDEX IF NOT EXISTS idx_recovery_status_expiry ON account_recovery_requests(status, expires_at);`); err != nil {
		return err
	}

	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS account_crypto_backups (
		user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		envelope TEXT NOT NULL,
		size INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		source_device_id TEXT NOT NULL
	); CREATE INDEX IF NOT EXISTS idx_crypto_backups_updated ON account_crypto_backups(updated_at DESC);`); err != nil {
		return err
	}

	// Older Phase 1-6 databases had no server sequence. Populate one once so
	// sync can use a monotonic cursor that is independent of timestamp ties.
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM messages WHERE server_seq IS NULL").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		rows, err := s.db.Query("SELECT id FROM messages WHERE server_seq IS NULL ORDER BY created_at ASC, id ASC")
		if err != nil {
			return err
		}
		ids := make([]string, 0, count)
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}

		var next int64
		if err := s.db.QueryRow("SELECT COALESCE(MAX(server_seq), 0) FROM messages").Scan(&next); err != nil {
			return err
		}
		if err := s.WithTxRetry(context.Background(), func(tx *sql.Tx) error {
			for _, id := range ids {
				next++
				if _, err := tx.Exec("UPDATE messages SET server_seq=? WHERE id=?", next, id); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(1), (2)`); err != nil {
		return err
	}
	if err := s.backfillSyncEvents(); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "recovery_code_hash", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "recovery_code_created_at", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "recovery_code_rotated_at", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "recovery_code_enabled", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "failed_login_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "last_failed_login_at", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("users", "locked_until", "TEXT"); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS device_identity_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		key_version INTEGER NOT NULL,
		public_key TEXT NOT NULL,
		created_at TEXT NOT NULL,
		retired_at TEXT,
		UNIQUE(device_id, key_version),
		UNIQUE(device_id, public_key)
	); CREATE INDEX IF NOT EXISTS idx_device_identity_keys_device ON device_identity_keys(device_id, key_version);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO device_identity_keys(device_id,key_version,public_key,created_at)
		SELECT id,COALESCE(identity_key_version,1),identity_public_key,created_at FROM devices
		WHERE COALESCE(identity_public_key,'') <> ''`); err != nil {
		return err
	}

	for _, col := range []struct{ name, def string }{
		{"phone_number", "TEXT NOT NULL DEFAULT ''"},
		{"phone_visibility", "TEXT NOT NULL DEFAULT 'contacts'"},
		{"discoverable_by_phone", "INTEGER NOT NULL DEFAULT 1"},
		{"discoverable_by_name", "INTEGER NOT NULL DEFAULT 1"},
		{"directory_sync_enabled", "INTEGER NOT NULL DEFAULT 1"},
		{"profile_version", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.ensureColumn("profiles", col.name, col.def); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_profiles_phone_unique ON profiles(phone_number) WHERE phone_number <> ''`); err != nil {
		return err
	}

	// Phase 42 records the schema state explicitly so future production migrations
	// can be applied incrementally instead of relying only on CREATE IF NOT EXISTS.
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(42)`); err != nil {
		return err
	}

	// Phase 43: legacy shared peer keys are no longer used by any endpoint.
	// Purge them once, rather than destructively deleting them on every startup.
	var schemaVersion int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&schemaVersion); err != nil {
		return err
	}
	if schemaVersion < 43 {
		if _, err := s.db.Exec(`DELETE FROM peer_keys`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(43)`); err != nil {
			return err
		}
		schemaVersion = 43
	}

	// Phase 44: directory profile discovery fields/indexes are ensured above and
	// this marker makes the client/server directory capability explicit in the
	// persistent migration history.
	if schemaVersion < 44 {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(44)`); err != nil {
			return err
		}
		schemaVersion = 44
	}

	// Phase 45: encrypted group-key envelopes are stored server-side. The
	// server never receives plaintext group keys; it stores only client-encrypted
	// envelopes addressed to current group members.
	if schemaVersion < 45 {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(45)`); err != nil {
			return err
		}
		schemaVersion = 45
	}

	// Phase 46 is retained as historical migration history only. Application
	// payload encryption for groups has been retired; never purge plaintext
	// group messages or require a cryptographic rekey during startup.
	if schemaVersion < 46 {
		if err := s.ensureColumn("groups", "key_version", "INTEGER NOT NULL DEFAULT 1"); err != nil {
			return err
		}
		if err := s.ensureColumn("groups", "rekey_required", "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
		if _, err := s.db.Exec(`UPDATE groups SET rekey_required=0`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(46)`); err != nil {
			return err
		}
		schemaVersion = 46
	}

	// Phase 47: server-assisted attachment E2E metadata. Logical plaintext
	// metadata remains queryable, while physical storage is ciphertext-only.
	if schemaVersion < 47 {
		for _, spec := range []struct{ table, column, definition string }{
			{"files", "storage_size", "INTEGER NOT NULL DEFAULT 0"},
			{"files", "crypto_version", "TEXT NOT NULL DEFAULT ''"},
			{"files", "crypto_scope", "TEXT NOT NULL DEFAULT ''"},
			{"files", "crypto_key_version", "INTEGER NOT NULL DEFAULT 0"},
			{"files", "crypto_nonce", "TEXT"},
			{"files", "crypto_mac", "TEXT"},
			{"files", "thumbnail_crypto_nonce", "TEXT"},
			{"files", "thumbnail_crypto_mac", "TEXT"},
		} {
			if err := s.ensureColumn(spec.table, spec.column, spec.definition); err != nil {
				return err
			}
		}
		if _, err := s.db.Exec(`UPDATE files SET storage_size=CASE WHEN storage_size=0 THEN size ELSE storage_size END`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(47)`); err != nil {
			return err
		}
		schemaVersion = 47
	}
	if schemaVersion < 48 {
		if err := s.ensureColumn("admin_audit", "admin_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS admin_accounts (
			id TEXT PRIMARY KEY, username TEXT NOT NULL COLLATE NOCASE UNIQUE, password_salt TEXT NOT NULL, password_hash TEXT NOT NULL, password_iterations INTEGER NOT NULL, role TEXT NOT NULL DEFAULT 'operator', status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_login_at TEXT
		); CREATE TABLE IF NOT EXISTS admin_sessions (
			id TEXT PRIMARY KEY, admin_id TEXT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE, token_hash TEXT NOT NULL, created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT
		); CREATE INDEX IF NOT EXISTS idx_admin_sessions_admin ON admin_sessions(admin_id, revoked_at, expires_at); CREATE INDEX IF NOT EXISTS idx_admin_sessions_token ON admin_sessions(token_hash, revoked_at, expires_at);`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(48)`); err != nil {
			return err
		}
		schemaVersion = 48
	}

	// Phase 49: security-rate-limit persistence, physical thumbnail accounting,
	// and migration-ledger reconciliation. Older releases used idempotent
	// ensureColumn/CREATE IF NOT EXISTS operations without recording every
	// historical step. We record those already-applied schema states once so
	// future migrations remain strictly contiguous from this point forward.
	if schemaVersion < 49 {
		if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS security_rate_limits (
			scope TEXT NOT NULL,
			rate_key TEXT NOT NULL,
			window_started_at TEXT NOT NULL,
			count INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(scope, rate_key)
		); CREATE INDEX IF NOT EXISTS idx_security_rate_limits_updated ON security_rate_limits(updated_at);`); err != nil {
			return err
		}
		if err := s.ensureColumn("files", "thumbnail_storage_size", "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
		rows, err := s.db.Query(`SELECT id,storage_path,storage_size,thumbnail_path,thumbnail_storage_size FROM files`)
		if err != nil {
			return err
		}
		type physicalRow struct {
			id                   string
			storagePath          string
			storageSize          int64
			thumbnailPath        string
			thumbnailStorageSize int64
		}
		var physicalRows []physicalRow
		for rows.Next() {
			var x physicalRow
			if err := rows.Scan(&x.id, &x.storagePath, &x.storageSize, &x.thumbnailPath, &x.thumbnailStorageSize); err != nil {
				rows.Close()
				return err
			}
			physicalRows = append(physicalRows, x)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if err := s.WithTxRetry(context.Background(), func(tx *sql.Tx) error {
			for _, x := range physicalRows {
				storageSize := x.storageSize
				if info, statErr := os.Stat(x.storagePath); statErr == nil && info.Mode().IsRegular() {
					storageSize = info.Size()
				}
				thumbnailStorageSize := x.thumbnailStorageSize
				if x.thumbnailPath != "" {
					if info, statErr := os.Stat(x.thumbnailPath); statErr == nil && info.Mode().IsRegular() {
						thumbnailStorageSize = info.Size()
					} else {
						thumbnailStorageSize = 0
					}
				} else {
					thumbnailStorageSize = 0
				}
				if _, err := tx.Exec(`UPDATE files SET storage_size=?,thumbnail_storage_size=? WHERE id=?`, storageSize, thumbnailStorageSize, x.id); err != nil {
					return err
				}
			}
			// Reconcile only the ledger markers that represent schema states already
			// embodied by this tree. Do not fabricate executable historical migrations.
			for version := 3; version <= 48; version++ {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(?)`, version); err != nil {
					return err
				}
			}
			_, err := tx.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(49)`)
			return err
		}); err != nil {
			return err
		}
		schemaVersion = 49
	}

	// Phase 58 / schema migration 50: enforce referential integrity for the
	// historical tables whose schemas predate explicit foreign-key constraints.
	// Devices are revoked rather than deleted by production code, so these
	// relationships are stable and safe to enforce. Existing orphaned rows are
	// rejected during startup instead of silently being discarded.
	if schemaVersion < 50 {
		var orphanCount int
		checks := []struct {
			name  string
			query string
		}{
			{"messages.sender", `SELECT COUNT(*) FROM messages m LEFT JOIN devices d ON d.id=m.sender_id WHERE d.id IS NULL`},
			{"messages.recipient", `SELECT COUNT(*) FROM messages m LEFT JOIN devices d ON d.id=m.recipient_id WHERE d.id IS NULL`},
			{"sync_events.device", `SELECT COUNT(*) FROM sync_events s LEFT JOIN devices d ON d.id=s.device_id WHERE d.id IS NULL`},
			{"crypto_backups.source_device", `SELECT COUNT(*) FROM account_crypto_backups b LEFT JOIN devices d ON d.id=b.source_device_id WHERE d.id IS NULL`},
		}
		for _, check := range checks {
			if err := s.db.QueryRow(check.query).Scan(&orphanCount); err != nil {
				return fmt.Errorf("phase 58 integrity check %s: %w", check.name, err)
			}
			if orphanCount != 0 {
				return fmt.Errorf("phase 58 integrity check %s found %d orphan rows", check.name, orphanCount)
			}
		}
		if _, err := s.db.Exec(`
CREATE TRIGGER IF NOT EXISTS trg_messages_sender_device_fk
BEFORE INSERT ON messages
BEGIN
    SELECT RAISE(ABORT, 'foreign key constraint failed')
    WHERE NOT EXISTS (SELECT 1 FROM devices WHERE id=NEW.sender_id);
END;
CREATE TRIGGER IF NOT EXISTS trg_messages_sender_device_fk_update
BEFORE UPDATE OF sender_id ON messages
BEGIN
    SELECT RAISE(ABORT, 'foreign key constraint failed')
    WHERE NOT EXISTS (SELECT 1 FROM devices WHERE id=NEW.sender_id);
END;
CREATE TRIGGER IF NOT EXISTS trg_messages_recipient_device_fk
BEFORE INSERT ON messages
BEGIN
    SELECT RAISE(ABORT, 'foreign key constraint failed')
    WHERE NOT EXISTS (SELECT 1 FROM devices WHERE id=NEW.recipient_id);
END;
CREATE TRIGGER IF NOT EXISTS trg_messages_recipient_device_fk_update
BEFORE UPDATE OF recipient_id ON messages
BEGIN
    SELECT RAISE(ABORT, 'foreign key constraint failed')
    WHERE NOT EXISTS (SELECT 1 FROM devices WHERE id=NEW.recipient_id);
END;
CREATE TRIGGER IF NOT EXISTS trg_sync_events_device_fk
BEFORE INSERT ON sync_events
BEGIN
    SELECT RAISE(ABORT, 'foreign key constraint failed')
    WHERE NOT EXISTS (SELECT 1 FROM devices WHERE id=NEW.device_id);
END;
CREATE TRIGGER IF NOT EXISTS trg_sync_events_device_fk_update
BEFORE UPDATE OF device_id ON sync_events
BEGIN
    SELECT RAISE(ABORT, 'foreign key constraint failed')
    WHERE NOT EXISTS (SELECT 1 FROM devices WHERE id=NEW.device_id);
END;
CREATE TRIGGER IF NOT EXISTS trg_crypto_backup_source_device_fk
BEFORE INSERT ON account_crypto_backups
BEGIN
    SELECT RAISE(ABORT, 'foreign key constraint failed')
    WHERE NOT EXISTS (SELECT 1 FROM devices WHERE id=NEW.source_device_id);
END;
CREATE TRIGGER IF NOT EXISTS trg_crypto_backup_source_device_fk_update
BEFORE UPDATE OF source_device_id ON account_crypto_backups
BEGIN
    SELECT RAISE(ABORT, 'foreign key constraint failed')
    WHERE NOT EXISTS (SELECT 1 FROM devices WHERE id=NEW.source_device_id);
END;
CREATE INDEX IF NOT EXISTS idx_messages_sender_created ON messages(sender_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_messages_recipient_created ON messages(recipient_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_sync_events_event ON sync_events(event_type, event_id);
`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES(50)`); err != nil {
			return err
		}
		schemaVersion = 50
	}

	// Phase 59 activates the strict migration ledger. Versions 1-50 represent
	// the legacy schema history produced by earlier releases; they are preserved
	// in schema_migrations for backward compatibility, while version 50 is the
	// explicit baseline from which strictly ordered migrations are enforced.
	if err := s.applyOrderedMigrations(); err != nil {
		return err
	}
	return nil
}

const (
	legacyMigrationBaselineVersion = 50
	strictMigrationVersion         = 51
	strictMigrationName            = "strict-ordered-migration-framework"
	strictMigrationChecksum        = "187733434b6f566de4423db44dce4d49033f37f3d67b9638656e4564d6d8ae13"
)

// applyOrderedMigrations applies all migrations defined in orderedMigrationSpecs
// one-by-one. Each migration is transactionally committed together with its
// compatibility marker and immutable checksum. If another process wins the
// migration race, the transaction is retried and the already-applied version
// is observed rather than executing the migration twice.
func (s *Store) applyOrderedMigrations() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migration_ledger (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    checksum TEXT NOT NULL,
    predecessor INTEGER NOT NULL,
    applied_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_schema_migration_ledger_applied ON schema_migration_ledger(applied_at, version);
`); err != nil {
		return err
	}

	if err := s.validateLegacyMigrationHistory(); err != nil {
		return err
	}

	knownVersion := legacyMigrationBaselineVersion
	for _, spec := range orderedMigrationSpecs() {
		if spec.Version > knownVersion {
			knownVersion = spec.Version
		}
	}
	var current int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	if current > knownVersion {
		return fmt.Errorf("database schema version %d is newer than this binary supports (latest known migration %d)", current, knownVersion)
	}

	// The legacy tree is treated as one immutable baseline rather than inventing
	// executable historical migrations that no longer exist in source control.
	if err := s.WithTxRetry(context.Background(), func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM schema_migration_ledger WHERE version=?`, legacyMigrationBaselineVersion).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			_, err := tx.Exec(`INSERT INTO schema_migration_ledger(version,name,checksum,predecessor,applied_at) VALUES(?,?,?,?,CURRENT_TIMESTAMP)`,
				legacyMigrationBaselineVersion, "legacy-schema-baseline-50", "legacy-baseline-50", legacyMigrationBaselineVersion-1)
			return err
		}
		return s.validateLedgerRowTx(tx, legacyMigrationBaselineVersion, "legacy-schema-baseline-50", "legacy-baseline-50", legacyMigrationBaselineVersion-1)
	}); err != nil {
		return err
	}

	for _, spec := range orderedMigrationSpecs() {
		if err := s.applyOrderedMigration(spec); err != nil {
			return err
		}
	}

	return nil
}

type orderedMigrationSpec struct {
	Version     int
	Name        string
	Checksum    string
	Predecessor int
	Apply       func(*sql.Tx) error
}

func orderedMigrationSpecs() []orderedMigrationSpec {
	return []orderedMigrationSpec{{
		Version:     strictMigrationVersion,
		Name:        strictMigrationName,
		Checksum:    strictMigrationChecksum,
		Predecessor: legacyMigrationBaselineVersion,
		Apply: func(*sql.Tx) error {
			// Activation migration: the ledger table is created before entering the
			// ordered transaction so a crashed first-run can safely retry. Version 51
			// itself is the immutable activation marker for the strict framework.
			return nil
		},
	}, {
		Version:     52,
		Name:        "admin-panel-settings",
		Checksum:    "locallink-admin-panel-settings-v1",
		Predecessor: 51,
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS admin_settings (
				key TEXT PRIMARY KEY,
				value TEXT NOT NULL,
				updated_at TEXT NOT NULL
			); CREATE INDEX IF NOT EXISTS idx_admin_settings_updated ON admin_settings(updated_at);`)
			return err
		},
	}, {
		Version:     53,
		Name:        "admin-network-policies",
		Checksum:    "locallink-admin-network-policies-v1",
		Predecessor: 52,
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS admin_user_network_policies (
				user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
				network_preference TEXT,
				gateway_sharing_policy TEXT,
				wifi_radio_policy TEXT,
				version INTEGER NOT NULL DEFAULT 1,
				updated_by TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);
			CREATE INDEX IF NOT EXISTS idx_admin_user_network_policies_updated ON admin_user_network_policies(updated_at);
			CREATE TABLE IF NOT EXISTS admin_device_network_policies (
				device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
				network_preference TEXT,
				gateway_sharing_policy TEXT,
				wifi_radio_policy TEXT,
				version INTEGER NOT NULL DEFAULT 1,
				updated_by TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);
			CREATE INDEX IF NOT EXISTS idx_admin_device_network_policies_updated ON admin_device_network_policies(updated_at);`)
			return err
		},
	}, {
		Version:     54,
		Name:        "retire-group-payload-crypto",
		Checksum:    "locallink-retire-group-payload-crypto-v1",
		Predecessor: 53,
		Apply: func(tx *sql.Tx) error {
			// Group payloads are now plaintext application data. Keep the historical
			// key-version columns for backward-compatible schema shape, but disable
			// rekey state and remove the obsolete server-side envelope table.
			if _, err := tx.Exec(`UPDATE groups SET rekey_required=0`); err != nil {
				return err
			}
			_, err := tx.Exec(`DROP TABLE IF EXISTS group_key_envelopes`)
			return err
		},
	}}
}

func (s *Store) validateLegacyMigrationHistory() error {
	var maxVersion int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&maxVersion); err != nil {
		return err
	}
	if maxVersion < legacyMigrationBaselineVersion {
		return fmt.Errorf("schema migration history only reaches version %d; expected at least baseline %d", maxVersion, legacyMigrationBaselineVersion)
	}
	for version := 1; version <= maxVersion; version++ {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, version).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("schema migration history gap/duplicate at version %d: count=%d", version, count)
		}
	}
	return nil
}

func (s *Store) validateLedgerRowTx(tx *sql.Tx, version int, name, checksum string, predecessor int) error {
	var gotName, gotChecksum string
	var gotPredecessor int
	err := tx.QueryRow(`SELECT name,checksum,predecessor FROM schema_migration_ledger WHERE version=?`, version).Scan(&gotName, &gotChecksum, &gotPredecessor)
	if err != nil {
		return err
	}
	if gotName != name || gotChecksum != checksum || gotPredecessor != predecessor {
		return fmt.Errorf("migration ledger mismatch for version %d", version)
	}
	return nil
}

func (s *Store) applyOrderedMigration(spec orderedMigrationSpec) error {
	return s.WithTxRetry(context.Background(), func(tx *sql.Tx) error {
		var current int
		if err := tx.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&current); err != nil {
			return err
		}
		if current >= spec.Version {
			return s.validateLedgerRowTx(tx, spec.Version, spec.Name, spec.Checksum, spec.Predecessor)
		}
		if current != spec.Predecessor {
			return fmt.Errorf("migration ordering violation: version %d requires predecessor %d, database is at %d", spec.Version, spec.Predecessor, current)
		}
		var existing int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM schema_migration_ledger WHERE version=?`, spec.Version).Scan(&existing); err != nil {
			return err
		}
		if existing != 0 {
			return fmt.Errorf("migration %d has ledger state but is not marked applied", spec.Version)
		}
		if err := spec.Apply(tx); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, spec.Version); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO schema_migration_ledger(version,name,checksum,predecessor,applied_at) VALUES(?,?,?,?,CURRENT_TIMESTAMP)`, spec.Version, spec.Name, spec.Checksum, spec.Predecessor)
		return err
	})
}

func (s *Store) ensureColumn(table, column, definition string) error {
	var exists int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, column).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		_, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
		return err
	}
	return nil
}

func (s *Store) Count(table string) (int, error) {
	if table != "devices" && table != "messages" {
		return 0, fmt.Errorf("invalid table")
	}
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
	return n, err
}

func (s *Store) backfillSyncEvents() error {
	return s.WithTxRetry(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT id,sender_id,recipient_id FROM messages ORDER BY COALESCE(server_seq,0),created_at,id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, sender, recipient string
			if err := rows.Scan(&id, &sender, &recipient); err != nil {
				rows.Close()
				return err
			}
			if _, err = tx.Exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, sender, "message", id); err != nil {
				rows.Close()
				return err
			}
			if _, err = tx.Exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, recipient, "message", id); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		type gm struct{ id, gid string }
		var groups []gm
		rows, err = tx.Query(`SELECT id,group_id FROM group_messages ORDER BY created_at,id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var x gm
			if err := rows.Scan(&x.id, &x.gid); err != nil {
				rows.Close()
				return err
			}
			groups = append(groups, x)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, x := range groups {
			mrows, err := tx.Query(`SELECT device_id FROM group_members WHERE group_id=?`, x.gid)
			if err != nil {
				return err
			}
			var members []string
			for mrows.Next() {
				var d string
				if err := mrows.Scan(&d); err != nil {
					mrows.Close()
					return err
				}
				members = append(members, d)
			}
			if err := mrows.Err(); err != nil {
				mrows.Close()
				return err
			}
			mrows.Close()
			for _, d := range members {
				if _, err = tx.Exec(`INSERT OR IGNORE INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, d, "group_message", x.id); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
