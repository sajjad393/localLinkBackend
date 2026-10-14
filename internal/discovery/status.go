package discovery

import (
	"sync"
	"time"
)

// Status reports the live state of the LAN discovery beacon so the admin
// health panel can display real endpoint state instead of guessing.
var (
	statusMu        sync.RWMutex
	statusRunning   bool
	statusStartedAt string
	statusPort      int
	statusName      string
	statusProtocol  string
	statusLastQuery string
)

func markStarted(port int, name, protocol string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	statusRunning = true
	statusStartedAt = time.Now().UTC().Format(time.RFC3339)
	statusPort = port
	statusName = name
	statusProtocol = protocol
	statusLastQuery = ""
}

func markStopped() {
	statusMu.Lock()
	defer statusMu.Unlock()
	statusRunning = false
}

func markQuery() {
	statusMu.Lock()
	defer statusMu.Unlock()
	statusLastQuery = time.Now().UTC().Format(time.RFC3339)
}

// Status returns a snapshot of beacon state for health reporting.
func Status() map[string]any {
	statusMu.RLock()
	defer statusMu.RUnlock()
	out := map[string]any{
		"running":    statusRunning,
		"started_at": statusStartedAt,
		"port":       statusPort,
		"name":       statusName,
		"protocol":   statusProtocol,
	}
	if statusLastQuery != "" {
		out["last_query_at"] = statusLastQuery
	}
	return out
}
