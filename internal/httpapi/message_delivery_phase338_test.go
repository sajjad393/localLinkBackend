package httpapi

import (
	"testing"
	"time"
)

func TestInsertMessageAtPreservesCanonicalPlaintextTimestamp(t *testing.T) {
	st, err := newTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := New(st)
	now := "2026-09-30T00:00:00Z"
	for _, id := range []string{"android-a", "android-b"} {
		if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,status) VALUES(?,?,?,?,?,'active')`, id, id, "android", now, now); err != nil {
			t.Fatal(err)
		}
	}
	created := "2026-09-30T09:46:50.475382Z"
	stored, _, err := a.insertMessageAt("android-a", "android-b", "hello", created, "m1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CreatedAt != created {
		t.Fatalf("stored timestamp = %s, want %s", stored.CreatedAt, created)
	}
}

func TestNormalizeMessageCreatedAtRejectsFutureClockSkew(t *testing.T) {
	future := time.Now().UTC().Add(maxFutureMessageSkew + time.Second).Format(time.RFC3339Nano)
	if _, ok := normalizeMessageCreatedAt(future); ok {
		t.Fatal("future message timestamp should be rejected")
	}
}
