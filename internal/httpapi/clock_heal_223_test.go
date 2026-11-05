package httpapi

// Bundle 223 (A + Q1): counters and login locks written while the server
// clock was ahead must heal by themselves when the clock is put back.

import (
	"testing"
	"time"
)

func clockHealSetup(t *testing.T) *API {
	t.Helper()
	t.Setenv("LOCALLINK_ADMIN_TOKEN", "test-admin-token-223")
	_, a := newTestAPI(t)
	return a
}

func seedRateRow(t *testing.T, a *API, scope, key string, started, updated time.Time, count int) {
	t.Helper()
	_, err := a.store.DB().Exec(`INSERT INTO security_rate_limits(scope,rate_key,window_started_at,count,updated_at) VALUES(?,?,?,?,?)`,
		scope, rateLimitKeyHash(scope, key), started.UTC().Format(time.RFC3339Nano), count, updated.UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}

func readRateRow(t *testing.T, a *API, scope, key string) (time.Time, int, bool) {
	t.Helper()
	var startedText string
	var count int
	err := a.store.DB().QueryRow(`SELECT window_started_at,count FROM security_rate_limits WHERE scope=? AND rate_key=?`,
		scope, rateLimitKeyHash(scope, key)).Scan(&startedText, &count)
	if err != nil {
		return time.Time{}, 0, false
	}
	started, _ := time.Parse(time.RFC3339Nano, startedText)
	return started, count, true
}

// 223-s1: a presence row 3 h in the future with count 30 lets the next
// update through and starts again "now" with count 1.
func TestClockHeal223FutureWindowStartsAgain(t *testing.T) {
	a := clockHealSetup(t)
	future := time.Now().UTC().Add(3 * time.Hour)
	seedRateRow(t, a, "presence-device", "dev-1", future, time.Now().UTC(), 30)
	if !a.allowPersistentSecurityAction("presence-device", "dev-1", 30, time.Minute) {
		t.Fatal("a window that starts in the future must not refuse the phone")
	}
	started, count, ok := readRateRow(t, a, "presence-device", "dev-1")
	if !ok {
		t.Fatal("row missing")
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if started.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("window still starts in the future: %v", started)
	}
}

// 223-s2: a normal full window (now, count 30) still refuses update 31.
func TestClockHeal223NormalLimitUnchanged(t *testing.T) {
	a := clockHealSetup(t)
	now := time.Now().UTC()
	seedRateRow(t, a, "presence-device", "dev-2", now, now, 30)
	if a.allowPersistentSecurityAction("presence-device", "dev-2", 30, time.Minute) {
		t.Fatal("update 31 in the same minute must still be refused")
	}
	// A window just under 1 minute ahead (small clock drift) is still trusted.
	seedRateRow(t, a, "presence-device", "dev-3", now.Add(30*time.Second), now, 30)
	if a.allowPersistentSecurityAction("presence-device", "dev-3", 30, time.Minute) {
		t.Fatal("a 30 s drift must not reset the limit")
	}
}

// 223-s3: a row whose updated_at is 3 h in the future is removed by the
// cleanup that runs on every call.
func TestClockHeal223CleanupRemovesFutureRows(t *testing.T) {
	a := clockHealSetup(t)
	future := time.Now().UTC().Add(3 * time.Hour)
	seedRateRow(t, a, "recovery-reset", "someone", future, future, 5)
	_ = a.allowPersistentSecurityAction("presence-device", "other", 30, time.Minute)
	if _, _, ok := readRateRow(t, a, "recovery-reset", "someone"); ok {
		t.Fatal("future row must be removed by the cleanup")
	}
}

// 223-s4: a stuck "message-device" counter heals the same way.
func TestClockHeal223MessageCounterHeals(t *testing.T) {
	a := clockHealSetup(t)
	future := time.Now().UTC().Add(5 * time.Hour)
	seedRateRow(t, a, "message-device", "dev-4", future, time.Now().UTC(), 999)
	if !a.allowPersistentSecurityAction("message-device", "dev-4", 120, time.Minute) {
		t.Fatal("message counter stuck in the future must heal")
	}
}

func seedLoginUser(t *testing.T, a *API, id string, failed int, lastFailed, lockedUntil time.Time) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := a.store.DB().Exec(`INSERT INTO users(id,username,password_salt,password_hash,password_iterations,status,created_at,updated_at,failed_login_count,last_failed_login_at,locked_until) VALUES(?,?,'s','h',1,'active',?,?,?,?,?)`,
		id, id, now, now, failed, lastFailed.UTC().Format(time.RFC3339Nano), lockedUntil.UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}

// 223-s5 (Q1): a login lock days in the future (clock was ahead) is ignored;
// a normal 15-min lock still holds.
func TestClockHeal223LoginLockFromFutureIgnored(t *testing.T) {
	a := clockHealSetup(t)
	now := time.Now().UTC()
	seedLoginUser(t, a, "u-future", 8, now.Add(48*time.Hour), now.Add(48*time.Hour+15*time.Minute))
	if got := a.loginLockedUntil("u-future"); !got.IsZero() {
		t.Fatalf("lock from the future must be ignored, got %v", got)
	}
	seedLoginUser(t, a, "u-normal", 8, now, now.Add(10*time.Minute))
	if got := a.loginLockedUntil("u-normal"); got.IsZero() {
		t.Fatal("a normal lock must still hold")
	}
}

// 223-s6 (Q1): a "last failed login" in the future starts the count again,
// so one wrong password does not lock the account.
func TestClockHeal223FutureLastFailedStartsAgain(t *testing.T) {
	a := clockHealSetup(t)
	now := time.Now().UTC()
	seedLoginUser(t, a, "u-count", 7, now.Add(6*time.Hour), time.Time{})
	if locked := a.recordLoginFailure("u-count"); !locked.IsZero() {
		t.Fatal("one failure after a clock jump must not lock")
	}
	var count int
	if err := a.store.DB().QueryRow(`SELECT failed_login_count FROM users WHERE id='u-count'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}
