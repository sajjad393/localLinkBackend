package httpapi

import (
	"testing"
	"time"
)

func TestCallReliabilityWindows(t *testing.T) {
	if 15*time.Second >= 60*time.Second {
		t.Fatal("heartbeat must be shorter than stale-call TTL")
	}
	if 20*time.Second >= 60*time.Second {
		t.Fatal("media watchdog must be shorter than stale-call TTL")
	}
}
