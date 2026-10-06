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
