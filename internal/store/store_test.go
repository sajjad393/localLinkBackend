package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenCreatesRestrictiveDatabasePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode checks are not portable to Windows")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nested", "locallink.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() failed for a new database: %v", err)
	}
	defer st.Close()

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("database file was not created: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("database permissions = %o, want 600", mode)
	}
	dirInfo, err := os.Stat(filepath.Dir(dbPath))
	if err != nil {
		t.Fatalf("database directory was not created: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("database directory permissions = %o, want 700", mode)
	}
}

func TestPhase43PurgesLegacyPeerKeysOnce(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "locallink.db")

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("initial Open() failed: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at) VALUES(?,?,'android','now','now')`, id, id); err != nil {
			st.Close()
			t.Fatalf("failed to seed peer device: %v", err)
		}
	}
	if _, err := st.DB().Exec(`INSERT INTO peer_keys(device_a,device_b,shared_key,created_at) VALUES('a','b','legacy','now')`); err != nil {
		st.Close()
		t.Fatalf("failed to seed legacy peer key: %v", err)
	}
	if _, err := st.DB().Exec(`DELETE FROM schema_migrations WHERE version>=43`); err != nil {
		st.Close()
		t.Fatalf("failed to rewind migration marker: %v", err)
	}
	if _, err := st.DB().Exec(`DELETE FROM schema_migration_ledger WHERE version>=43`); err != nil {
		st.Close()
		t.Fatalf("failed to rewind migration ledger: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("initial Close() failed: %v", err)
	}

	st, err = Open(dbPath)
	if err != nil {
		t.Fatalf("re-open failed: %v", err)
	}
	defer st.Close()

	var count int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM peer_keys`).Scan(&count); err != nil {
		t.Fatalf("failed to count legacy peer keys: %v", err)
	}
	if count != 0 {
		t.Fatalf("legacy peer keys remaining = %d, want 0", count)
	}
	var version int
	if err := st.DB().QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("failed to read schema version: %v", err)
	}
	if version < 43 {
		t.Fatalf("schema version = %d, want >= 43", version)
	}
}

func TestPhase46PreservesLegacyPlaintextGroupMessages(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "locallink.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("initial Open() failed: %v", err)
	}
	if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at) VALUES('a','A','android','now','now')`); err != nil {
		st.Close()
		t.Fatalf("insert device: %v", err)
	}
	if _, err := st.DB().Exec(`INSERT INTO groups(id,name,owner_id,created_at) VALUES('g','Legacy','a','now')`); err != nil {
		st.Close()
		t.Fatalf("insert group: %v", err)
	}
	if _, err := st.DB().Exec(`INSERT INTO group_members(group_id,device_id,role,joined_at) VALUES('g','a','owner','now')`); err != nil {
		st.Close()
		t.Fatalf("insert member: %v", err)
	}
	if _, err := st.DB().Exec(`INSERT INTO group_messages(id,group_id,sender_id,body,created_at,server_seq) VALUES('legacy','g','a','plaintext','now',1)`); err != nil {
		st.Close()
		t.Fatalf("insert legacy group message: %v", err)
	}
	if _, err := st.DB().Exec(`INSERT INTO sync_events(device_id,event_type,event_id) VALUES('a','group_message','legacy')`); err != nil {
		st.Close()
		t.Fatalf("insert sync event: %v", err)
	}
	if _, err := st.DB().Exec(`DELETE FROM schema_migrations WHERE version>=46`); err != nil {
		st.Close()
		t.Fatalf("rewind migration: %v", err)
	}
	if _, err := st.DB().Exec(`DELETE FROM schema_migration_ledger WHERE version>=46`); err != nil {
		st.Close()
		t.Fatalf("rewind migration ledger: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	st, err = Open(dbPath)
	if err != nil {
		t.Fatalf("re-open failed: %v", err)
	}
	defer st.Close()
	var count int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM group_messages WHERE id='legacy'`).Scan(&count); err != nil {
		t.Fatalf("count legacy message: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy plaintext group message count = %d, want 1", count)
	}
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sync_events WHERE event_type='group_message' AND event_id='legacy'`).Scan(&count); err != nil {
		t.Fatalf("count legacy sync event: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy group sync event count = %d, want 1", count)
	}
	var version int
	if err := st.DB().QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version < 54 {
		t.Fatalf("schema version = %d, want >= 54", version)
	}
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='group_key_envelopes'`).Scan(&count); err != nil {
		t.Fatalf("count retired group-key table: %v", err)
	}
	if count != 0 {
		t.Fatalf("group_key_envelopes table still exists")
	}
}

func TestMigrationLedgerIsContiguousThroughPhase50(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var maxVersion int
	if err := st.DB().QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&maxVersion); err != nil {
		t.Fatal(err)
	}
	if maxVersion < 50 {
		t.Fatalf("max migration version = %d, want >= 50", maxVersion)
	}
	for v := 1; v <= maxVersion; v++ {
		var count int
		if err := st.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, v).Scan(&count); err != nil {
			t.Fatalf("migration %d lookup failed: %v", v, err)
		}
		if count != 1 {
			t.Fatalf("migration %d marker count = %d, want 1", v, count)
		}
	}
	for _, name := range []string{"thumbnail_storage_size", "crypto_version", "crypto_scope", "crypto_key_version", "crypto_nonce", "crypto_mac", "thumbnail_crypto_nonce", "thumbnail_crypto_mac"} {
		var count int
		if err := st.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('files') WHERE name=?`, name).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("files.%s missing", name)
		}
	}
	var limiterTable int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='security_rate_limits'`).Scan(&limiterTable); err != nil {
		t.Fatal(err)
	}
	if limiterTable != 1 {
		t.Fatal("security_rate_limits table missing")
	}
}

func TestPhase49BackfillsPhysicalAttachmentSizes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "locallink.db")
	storagePath := filepath.Join(dir, "stored.bin")
	thumbPath := filepath.Join(dir, "stored.thumb")
	if err := os.WriteFile(storagePath, make([]byte, 137), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumbPath, make([]byte, 29), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at) VALUES('device-a','Backfill','android','now','now')`); err != nil {
		st.Close()
		t.Fatalf("seed owner device: %v", err)
	}
	if _, err := st.DB().Exec(`INSERT INTO files(id,owner_id,original_name,content_type,size,sha256,storage_path,thumbnail_path,created_at,status,storage_size,thumbnail_storage_size) VALUES('physical-backfill','device-a','x.bin','application/octet-stream',99,'sha',?,?,?,?,?,?)`, storagePath, thumbPath, "now", "ready", 0, 0); err != nil {
		st.Close()
		t.Fatalf("seed attachment: %v", err)
	}
	// Force the one-time migrations to run again on the existing database.
	if _, err := st.DB().Exec(`DELETE FROM schema_migrations WHERE version>=49`); err != nil {
		st.Close()
		t.Fatalf("rewind phase 49 marker: %v", err)
	}
	if _, err := st.DB().Exec(`DELETE FROM schema_migration_ledger WHERE version>=49`); err != nil {
		st.Close()
		t.Fatalf("rewind migration ledger: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var storageSize, thumbnailStorageSize int64
	if err := st.DB().QueryRow(`SELECT storage_size,thumbnail_storage_size FROM files WHERE id='physical-backfill'`).Scan(&storageSize, &thumbnailStorageSize); err != nil {
		t.Fatal(err)
	}
	if storageSize != 137 || thumbnailStorageSize != 29 {
		t.Fatalf("physical sizes = %d + %d, want 137 + 29", storageSize, thumbnailStorageSize)
	}
}

func TestPhase59StrictMigrationLedgerFreshInstall(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	defer st.Close()

	wantVersion := legacyMigrationBaselineVersion
	for _, spec := range orderedMigrationSpecs() {
		if spec.Version > wantVersion {
			wantVersion = spec.Version
		}
	}

	var maxVersion int
	if err := st.DB().QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&maxVersion); err != nil {
		t.Fatal(err)
	}
	if maxVersion != wantVersion {
		t.Fatalf("schema version=%d, want latest ordered migration %d", maxVersion, wantVersion)
	}

	wantLedger := 1 + len(orderedMigrationSpecs())
	var ledgerCount int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM schema_migration_ledger`).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != wantLedger {
		t.Fatalf("ledger rows=%d, want %d (baseline + ordered specs)", ledgerCount, wantLedger)
	}

	var name, checksum string
	var predecessor int
	if err := st.DB().QueryRow(`SELECT name,checksum,predecessor FROM schema_migration_ledger WHERE version=?`, strictMigrationVersion).Scan(&name, &checksum, &predecessor); err != nil {
		t.Fatal(err)
	}
	if name != strictMigrationName || checksum != strictMigrationChecksum || predecessor != legacyMigrationBaselineVersion {
		t.Fatalf("migration 51 ledger mismatch: name=%q checksum=%q predecessor=%d", name, checksum, predecessor)
	}
}

func TestPhase59StrictMigrationLedgerIdempotentAcrossRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "locallink.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var count int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM schema_migration_ledger WHERE version=?`, strictMigrationVersion).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migration 51 ledger count=%d, want 1", count)
	}
}

func TestPhase59RejectsLegacySchemaGap(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "locallink.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`DELETE FROM schema_migrations WHERE version=50`); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(dbPath); err == nil {
		t.Fatal("expected Open() to reject a migration history gap")
	}
}

func TestPhase59RejectsLedgerChecksumDrift(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "locallink.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE schema_migration_ledger SET checksum='tampered' WHERE version=?`, strictMigrationVersion); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(dbPath); err == nil {
		t.Fatal("expected Open() to reject migration checksum drift")
	}
}

func TestPhase59RejectsDatabaseFromFutureMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "locallink.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	futureVersion := legacyMigrationBaselineVersion
	for _, spec := range orderedMigrationSpecs() {
		if spec.Version > futureVersion {
			futureVersion = spec.Version
		}
	}
	futureVersion++
	if _, err := st.DB().Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, futureVersion); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(dbPath); err == nil {
		t.Fatal("expected Open() to reject a database newer than the binary")
	}
}

func TestPhase59OrderedMigrationSpecsAreStrictlySequential(t *testing.T) {
	specs := orderedMigrationSpecs()
	if len(specs) == 0 {
		t.Fatal("no ordered migration specs registered")
	}
	previous := legacyMigrationBaselineVersion
	for _, spec := range specs {
		if spec.Version != previous+1 {
			t.Fatalf("migration %d predecessor=%d, expected predecessor %d", spec.Version, spec.Predecessor, previous)
		}
		if spec.Predecessor != previous {
			t.Fatalf("migration %d predecessor=%d, want %d", spec.Version, spec.Predecessor, previous)
		}
		if spec.Name == "" || spec.Checksum == "" || spec.Apply == nil {
			t.Fatalf("migration %d has incomplete specification", spec.Version)
		}
		previous = spec.Version
	}
}

func TestPhase58SQLiteDurabilityAndContentionPragmas(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	checks := []struct {
		name  string
		query string
		want  string
	}{
		{"journal_mode", `PRAGMA journal_mode`, "wal"},
		{"synchronous", `PRAGMA synchronous`, "2"},
		{"foreign_keys", `PRAGMA foreign_keys`, "1"},
		{"busy_timeout", `PRAGMA busy_timeout`, "10000"},
	}
	for _, tc := range checks {
		var got string
		if err := st.DB().QueryRow(tc.query).Scan(&got); err != nil {
			t.Fatalf("%s query failed: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s=%q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPhase58ReferentialIntegrityTriggersRejectOrphans(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := "2026-01-01T00:00:00Z"
	for _, id := range []string{"a", "b"} {
		if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at) VALUES(?,?,?,?,?)`, id, id, "android", now, now); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name  string
		query string
		args  []any
	}{
		{"missing sender", `INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status) VALUES(?,?,?,?,?,'sent')`, []any{"m1", "missing", "b", "e2e:v2:test", now}},
		{"missing recipient", `INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status) VALUES(?,?,?,?,?,'sent')`, []any{"m2", "a", "missing", "e2e:v2:test", now}},
		{"missing sync device", `INSERT INTO sync_events(device_id,event_type,event_id) VALUES(?,?,?)`, []any{"missing", "message", "m1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := st.DB().Exec(tc.query, tc.args...); err == nil {
				t.Fatalf("expected referential-integrity rejection")
			}
		})
	}

	if _, err := st.DB().Exec(`INSERT INTO messages(id,sender_id,recipient_id,body,created_at,status) VALUES('valid','a','b','e2e:v2:test',?,'sent')`, now); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}
	if _, err := st.DB().Exec(`INSERT INTO sync_events(device_id,event_type,event_id) VALUES('a','message','valid')`); err != nil {
		t.Fatalf("valid sync event rejected: %v", err)
	}
}

func TestPhase58IntegrityReport(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	report, err := st.IntegrityReport()
	if err != nil {
		t.Fatal(err)
	}
	if report.IntegrityCheck != "ok" {
		t.Fatalf("quick_check=%q, want ok", report.IntegrityCheck)
	}
	if len(report.ForeignKeyErrors) != 0 {
		t.Fatalf("foreign key errors = %v, want none", report.ForeignKeyErrors)
	}
}

func TestPhase58WithTxRetryDoesNotReplayNonBusyFailures(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	called := 0
	wantErr := fmt.Errorf("validation failed")
	err = st.WithTxRetry(context.Background(), func(tx *sql.Tx) error {
		called++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want %v", err, wantErr)
	}
	if called != 1 {
		t.Fatalf("callback called %d times, want 1", called)
	}
}

func TestPhase58WithTxRetryRetriesBusyFailures(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	called := 0
	err = st.WithTxRetry(context.Background(), func(tx *sql.Tx) error {
		called++
		if called < 3 {
			return fmt.Errorf("database is locked")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTxRetry failed: %v", err)
	}
	if called != 3 {
		t.Fatalf("callback called %d times, want 3", called)
	}
}
