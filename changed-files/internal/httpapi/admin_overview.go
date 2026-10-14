package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"locallink/backend/internal/discovery"
)

// ---------------------------------------------------------------------------
// Design note
//
// This file backs the redesigned admin dashboard. Every number served here is
// derived from LocalLink's own tables, the runtime metrics sampler, live
// process/host readings, or subsystem state. Nothing is fabricated: when a
// reading is unavailable the field is omitted and the component is reported as
// "unknown".
//
// Privacy boundary (unchanged): operational metadata only. No message bodies,
// no passwords, no keys, no media content.
// ---------------------------------------------------------------------------

const (
	// Presence TTL mirrors the rest of the admin surface.
	overviewPresenceTTL = 90 * time.Second
)

// ---------------------------------------------------------------------------
// Analytics
// ---------------------------------------------------------------------------

type analyticsWindow struct {
	Key           string
	Duration      time.Duration
	BucketSeconds int64
}

var analyticsWindows = map[string]analyticsWindow{
	"24h": {Key: "24h", Duration: 24 * time.Hour, BucketSeconds: 3600},
	"7d":  {Key: "7d", Duration: 7 * 24 * time.Hour, BucketSeconds: 6 * 3600},
	"30d": {Key: "30d", Duration: 30 * 24 * time.Hour, BucketSeconds: 24 * 3600},
	"90d": {Key: "90d", Duration: 90 * 24 * time.Hour, BucketSeconds: 24 * 3600},
}

type seriesPoint struct {
	T string `json:"t"`
	V int64  `json:"v"`
}

// countSeries buckets rows of table by the timestamp column. table and column
// are internal constants, never user input.
func (a *API) countSeries(table, column string, from time.Time, bucketSeconds int64) ([]seriesPoint, error) {
	query := fmt.Sprintf(`SELECT (CAST(strftime('%%s', %s) AS INTEGER) / ?) * ? AS b, COUNT(*)
		FROM %s
		WHERE %s >= ? AND strftime('%%s', %s) IS NOT NULL
		GROUP BY b ORDER BY b`, column, table, column, column)
	rows, err := a.store.DB().Query(query, bucketSeconds, bucketSeconds, from.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []seriesPoint{}
	for rows.Next() {
		var bucket int64
		var count int64
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, err
		}
		out = append(out, seriesPoint{T: time.Unix(bucket, 0).UTC().Format(time.RFC3339), V: count})
	}
	return out, rows.Err()
}

func (a *API) sumSeries(table, column, sumColumn string, from time.Time, bucketSeconds int64) ([]seriesPoint, error) {
	query := fmt.Sprintf(`SELECT (CAST(strftime('%%s', %s) AS INTEGER) / ?) * ? AS b, COALESCE(SUM(%s),0)
		FROM %s
		WHERE %s >= ? AND strftime('%%s', %s) IS NOT NULL
		GROUP BY b ORDER BY b`, column, sumColumn, table, column, column)
	rows, err := a.store.DB().Query(query, bucketSeconds, bucketSeconds, from.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []seriesPoint{}
	for rows.Next() {
		var bucket, total int64
		if err := rows.Scan(&bucket, &total); err != nil {
			return nil, err
		}
		out = append(out, seriesPoint{T: time.Unix(bucket, 0).UTC().Format(time.RFC3339), V: total})
	}
	return out, rows.Err()
}

// emptySeries builds a complete, zero-filled series so charts never show gaps.
func emptySeries(from, to time.Time, bucketSeconds int64) []seriesPoint {
	out := []seriesPoint{}
	start := from.UTC().Truncate(time.Duration(bucketSeconds) * time.Second)
	end := to.UTC()
	for t := start; !t.After(end); t = t.Add(time.Duration(bucketSeconds) * time.Second) {
		out = append(out, seriesPoint{T: t.Format(time.RFC3339), V: 0})
	}
	return out
}

func mergeSeries(base []seriesPoint, extra []seriesPoint) []seriesPoint {
	if len(base) == 0 {
		return extra
	}
	index := make(map[string]int, len(base))
	for i, p := range base {
		index[p.T] = i
	}
	for _, p := range extra {
		if i, ok := index[p.T]; ok {
			base[i].V += p.V
		}
	}
	return base
}

// runtimeBucket reads the metrics sampler output if the server installed one.
func (a *API) runtimeSnapshot() (map[string]any, bool) {
	a.runtimeMu.RLock()
	provider := a.runtimeProvider
	a.runtimeMu.RUnlock()
	if provider == nil {
		return nil, false
	}
	snap := provider()
	if snap == nil {
		return nil, false
	}
	return snap, true
}

func number(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case uint64:
		return int64(n), true
	case float64:
		return int64(n), true
	case int32:
		return int64(n), true
	default:
		return 0, false
	}
}

// runtimeSeries converts sampler buckets into a series aligned to the window.
func runtimeSeries(snapshot map[string]any, key string, from, to time.Time, bucketSeconds int64) ([]seriesPoint, bool) {
	raw, ok := snapshot["buckets"].([]map[string]any)
	if !ok {
		if alt, ok2 := snapshot["buckets"].([]any); ok2 {
			raw = make([]map[string]any, 0, len(alt))
			for _, item := range alt {
				if m, ok3 := item.(map[string]any); ok3 {
					raw = append(raw, m)
				}
			}
		} else {
			return nil, false
		}
	}
	buckets := map[int64]int64{}
	for _, b := range raw {
		ts, _ := b["t"].(string)
		when, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		if when.Before(from) || when.After(to) {
			continue
		}
		value, _ := number(b[key])
		slot := when.Unix() / bucketSeconds * bucketSeconds
		buckets[slot] += value
	}
	out := []seriesPoint{}
	start := from.UTC().Truncate(time.Duration(bucketSeconds) * time.Second)
	for t := start; !t.After(to.UTC()); t = t.Add(time.Duration(bucketSeconds) * time.Second) {
		out = append(out, seriesPoint{T: t.Format(time.RFC3339), V: buckets[t.Unix()]})
	}
	return out, true
}

// AdminAnalytics serves bucketed time series for the dashboard charts.
// GET /api/v1/admin/analytics?range=24h|7d|30d|90d
func (a *API) AdminAnalytics(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	key := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("range")))
	if key == "" {
		key = "24h"
	}
	window, ok := analyticsWindows[key]
	if !ok {
		writeError(w, http.StatusBadRequest, "range must be one of 24h, 7d, 30d, 90d")
		return
	}

	now := time.Now().UTC()
	from := now.Add(-window.Duration)
	bucket := window.BucketSeconds

	series := map[string][]seriesPoint{}
	totals := map[string]int64{}

	specs := []struct {
		key    string
		table  string
		column string
	}{
		{"messages", "messages", "created_at"},
		{"group_messages", "group_messages", "created_at"},
		{"users", "users", "created_at"},
		{"devices", "devices", "created_at"},
		{"calls", "calls", "started_at"},
		{"files", "files", "created_at"},
		{"security_events", "security_audit", "created_at"},
		{"admin_actions", "admin_audit", "created_at"},
	}
	for _, spec := range specs {
		points, err := a.countSeries(spec.table, spec.column, from, bucket)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "analytics query failed: "+spec.key)
			return
		}
		series[spec.key] = mergeSeries(emptySeries(from, now, bucket), points)
		var total int64
		for _, p := range series[spec.key] {
			total += p.V
		}
		totals[spec.key] = total
	}

	bytesPoints, err := a.sumSeries("files", "created_at", "size", from, bucket)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics query failed: file_bytes")
		return
	}
	series["file_bytes"] = mergeSeries(emptySeries(from, now, bucket), bytesPoints)
	var byteTotal int64
	for _, p := range series["file_bytes"] {
		byteTotal += p.V
	}
	totals["file_bytes"] = byteTotal

	runtimeInfo := map[string]any{"available": false}
	if snapshot, ok := a.runtimeSnapshot(); ok {
		runtimeInfo = map[string]any{"available": true}
		for _, key := range []string{"requests", "client_errors", "server_errors", "bytes_out", "recovered_panics", "uptime_seconds"} {
			if v, ok := number(snapshot[key]); ok {
				runtimeInfo[key] = v
			}
		}
		if points, ok := runtimeSeries(snapshot, "requests", from, now, bucket); ok {
			series["requests"] = points
		}
		if points, ok := runtimeSeries(snapshot, "errors", from, now, bucket); ok {
			series["errors"] = points
		}
		if points, ok := runtimeSeries(snapshot, "bytes_out", from, now, bucket); ok {
			series["network_bytes"] = points
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"range":          window.Key,
		"bucket_seconds": bucket,
		"from":           from.Format(time.RFC3339),
		"to":             now.Format(time.RFC3339),
		"generated_at":   now.Format(time.RFC3339),
		"series":         series,
		"totals":         totals,
		"runtime":        runtimeInfo,
	})
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

type healthComponent struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Status    string   `json:"status"`
	Value     *float64 `json:"value,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
	Trend     string   `json:"trend,omitempty"`
	Detail    string   `json:"detail,omitempty"`
	UpdatedAt string   `json:"updated_at"`
}

func floatPtr(v float64) *float64 { return &v }

// statusForThreshold maps a utilisation percentage to a status.
func statusForThreshold(value, warn, critical float64) string {
	switch {
	case value >= critical:
		return "critical"
	case value >= warn:
		return "warning"
	default:
		return "healthy"
	}
}

func worstStatus(statuses []string) string {
	rank := map[string]int{"unknown": 0, "healthy": 1, "warning": 2, "critical": 3, "offline": 4}
	worst := "healthy"
	for _, s := range statuses {
		if rank[s] > rank[worst] {
			worst = s
		}
	}
	return worst
}

type resourceSample struct {
	At      time.Time
	CPU     float64
	CPUOK   bool
	Mem     uint64
	MemOK   bool
}

func (a *API) recordResourceSample(sample resourceSample) []resourceSample {
	a.resMu.Lock()
	defer a.resMu.Unlock()
	now := time.Now()
	if len(a.resHistory) == 0 || now.Sub(a.resHistory[len(a.resHistory)-1].At) >= 5*time.Second {
		a.resHistory = append(a.resHistory, sample)
		if len(a.resHistory) > 240 {
			a.resHistory = a.resHistory[len(a.resHistory)-240:]
		}
	}
	out := make([]resourceSample, len(a.resHistory))
	copy(out, a.resHistory)
	return out
}

// trendFor reports a real trend by comparing a value with a measured earlier
// value; it returns "" when there is no history to compare against.
func trendFor(current, earlier float64, hasHistory bool) string {
	if !hasHistory {
		return ""
	}
	delta := current - earlier
	if delta > 1 {
		return "up"
	}
	if delta < -1 {
		return "down"
	}
	return "flat"
}

// AdminHealth reports subsystem health for the dashboard health panel.
// GET /api/v1/admin/health
func (a *API) AdminHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339)

	db := a.store.DB()
	components := []healthComponent{}

	// --- database ---------------------------------------------------------
	database := healthComponent{Key: "database", Label: "Database", Status: "healthy", UpdatedAt: stamp}
	if err := db.Ping(); err != nil {
		database.Status = "critical"
		database.Detail = "database is not responding"
	} else {
		var tables int
		_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&tables)
		size := int64(0)
		if info, err := os.Stat(a.dbPath()); err == nil {
			size = info.Size()
		}
		database.Value = floatPtr(float64(size))
		database.Unit = "bytes"
		database.Detail = fmt.Sprintf("SQLite responding, %s, %d tables", humanBytes(size), tables)
	}
	components = append(components, database)

	// --- process / host ---------------------------------------------------
	sample := SampleProcess(a.fileRoot)
	history := a.recordResourceSample(resourceSample{At: time.Now(), CPU: sample.CPUPercent, CPUOK: sample.CPUOK, Mem: sample.MemBytes, MemOK: sample.MemOK})
	var earlierCPU, earlierMem float64
	cpuHistory, memHistory := false, false
	if len(history) > 1 {
		for _, h := range history {
			if h.CPUOK && h.At.Before(time.Now().Add(-30*time.Second)) {
				earlierCPU = h.CPU
				cpuHistory = true
			}
			if h.MemOK && h.At.Before(time.Now().Add(-30*time.Second)) {
				earlierMem = float64(h.Mem)
				memHistory = true
			}
		}
	}

	cpu := healthComponent{Key: "cpu", Label: "CPU", UpdatedAt: stamp}
	if sample.CPUOK {
		cpu.Value = floatPtr(round1(sample.CPUPercent))
		cpu.Unit = "%"
		cpu.Threshold = floatPtr(85)
		cpu.Status = statusForThreshold(sample.CPUPercent, 70, 85)
		cpu.Detail = fmt.Sprintf("%.1f%% of %d cores", sample.CPUPercent, numCPUs())
		cpu.Trend = trendFor(sample.CPUPercent, earlierCPU, cpuHistory)
	} else {
		cpu.Status = "unknown"
		cpu.Detail = "CPU usage is not available on this platform"
	}
	components = append(components, cpu)

	memory := healthComponent{Key: "memory", Label: "Memory", UpdatedAt: stamp}
	if sample.MemOK && sample.MemTotal > 0 {
		pct := float64(sample.MemBytes) / float64(sample.MemTotal) * 100
		memory.Value = floatPtr(round1(pct))
		memory.Unit = "%"
		memory.Threshold = floatPtr(85)
		memory.Status = statusForThreshold(pct, 75, 90)
		memory.Trend = trendFor(float64(sample.MemBytes), earlierMem, memHistory)
		memory.Detail = fmt.Sprintf("%s of %s in use", humanBytes(int64(sample.MemBytes)), humanBytes(int64(sample.MemTotal)))
	} else if sample.MemOK {
		memory.Value = floatPtr(float64(sample.MemBytes))
		memory.Unit = "bytes"
		memory.Status = "healthy"
		memory.Detail = humanBytes(int64(sample.MemBytes)) + " resident"
	} else {
		memory.Status = "unknown"
		memory.Detail = "memory usage is not available on this platform"
	}
	components = append(components, memory)

	storage := healthComponent{Key: "storage", Label: "Storage", UpdatedAt: stamp}
	if sample.DiskOK && sample.DiskTotal > 0 {
		usedPct := float64(sample.DiskTotal-sample.DiskFree) / float64(sample.DiskTotal) * 100
		storage.Value = floatPtr(round1(usedPct))
		storage.Unit = "%"
		storage.Threshold = floatPtr(90)
		storage.Status = statusForThreshold(usedPct, 80, 90)
		storage.Detail = fmt.Sprintf("%s free of %s", humanBytes(int64(sample.DiskFree)), humanBytes(int64(sample.DiskTotal)))
	} else {
		storage.Status = "unknown"
		storage.Detail = "disk usage is not available on this platform"
	}
	components = append(components, storage)

	// --- websocket hub ----------------------------------------------------
	hub := healthComponent{Key: "websocket", Label: "WebSocket Hub", UpdatedAt: stamp}
	connections := len(a.hub.DeviceIDs())
	hub.Value = floatPtr(float64(connections))
	hub.Unit = "connections"
	if connections > 0 {
		hub.Status = "healthy"
		hub.Detail = fmt.Sprintf("%d device(s) connected for realtime delivery", connections)
	} else {
		hub.Status = "warning"
		hub.Detail = "no devices are connected for realtime delivery"
	}
	components = append(components, hub)

	// --- LAN discovery ----------------------------------------------------
	lan := healthComponent{Key: "lan_discovery", Label: "LAN Discovery", UpdatedAt: stamp}
	disco := discovery.Status()
	if running, _ := disco["running"].(bool); running {
		lan.Status = "healthy"
		port, _ := number(disco["port"])
		lan.Detail = fmt.Sprintf("beacon responding on UDP :%d", port)
	} else {
		lan.Status = "offline"
		lan.Detail = "discovery beacon is not running"
	}
	components = append(components, lan)

	// --- mesh coordination ------------------------------------------------
	cutoff := now.Add(-overviewPresenceTTL)
	meshCapable, lanAvailable, wifiDirect := 0, 0, 0
	a.presenceMu.RLock()
	for _, p := range a.presence {
		if p.UpdatedAt == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, p.UpdatedAt)
		if err != nil || t.Before(cutoff) {
			continue
		}
		if p.MeshAvailable {
			meshCapable++
		}
		if p.LanAvailable {
			lanAvailable++
		}
		if p.WiFiDirectConnected {
			wifiDirect++
		}
	}
	a.presenceMu.RUnlock()

	mesh := healthComponent{Key: "mesh", Label: "Mesh Coordination", UpdatedAt: stamp}
	mesh.Value = floatPtr(float64(meshCapable))
	mesh.Unit = "devices"
	if meshCapable > 0 {
		mesh.Status = "healthy"
		mesh.Detail = fmt.Sprintf("%d mesh-capable device(s) reporting; %d on LAN, %d on Wi-Fi Direct", meshCapable, lanAvailable, wifiDirect)
	} else {
		mesh.Status = "warning"
		mesh.Detail = "no mesh-capable devices are reporting"
	}
	components = append(components, mesh)

	// --- file service -----------------------------------------------------
	files := healthComponent{Key: "file_service", Label: "File Service", UpdatedAt: stamp}
	fileOK := true
	if info, err := os.Stat(a.fileRoot); err != nil || !info.IsDir() {
		fileOK = false
	}
	pendingUploads := 0
	_ = db.QueryRow(`SELECT COUNT(*) FROM files WHERE status IN ('uploading','processing','pending','in_progress')`).Scan(&pendingUploads)
	files.Value = floatPtr(float64(pendingUploads))
	files.Unit = "pending"
	if fileOK {
		files.Status = "healthy"
		files.Detail = fmt.Sprintf("storage writable at %s, %d pending transfer(s)", filepath.Base(a.fileRoot), pendingUploads)
	} else {
		files.Status = "critical"
		files.Detail = "attachment storage directory is missing"
	}
	components = append(components, files)

	// --- call service -----------------------------------------------------
	calls := healthComponent{Key: "call_service", Label: "Call Service", UpdatedAt: stamp}
	activeCalls := 0
	_ = db.QueryRow(`SELECT COUNT(*) FROM calls WHERE status IN ('ringing','connected')`).Scan(&activeCalls)
	calls.Value = floatPtr(float64(activeCalls))
	calls.Unit = "active"
	if connections > 0 {
		calls.Status = "healthy"
		calls.Detail = fmt.Sprintf("signalling online, %d active call(s)", activeCalls)
	} else {
		calls.Status = "warning"
		calls.Detail = fmt.Sprintf("signalling idle (no connected devices), %d active call(s)", activeCalls)
	}
	components = append(components, calls)

	statuses := make([]string, 0, len(components))
	for _, c := range components {
		statuses = append(statuses, c.Status)
	}

	overall := worstStatus(statuses)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"overall":      overall,
		"generated_at": stamp,
		"components":   components,
	})
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

func humanBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	for _, u := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}

// ---------------------------------------------------------------------------
// Security summary
// ---------------------------------------------------------------------------

// securitySeverity classifies observed audit actions. The mapping is a fixed
// rule over real action names, not an invented score.
func securitySeverity(action string) string {
	critical := map[string]bool{
		"device_revoked":           true,
		"session_revoked":          true,
		"admin_session_revoked":    true,
		"admin_user_status_changed": true,
		"recovery_code_disable":    true,
	}
	warning := map[string]bool{
		"login_failed":        true,
		"rotate_identity_key": true,
		"recovery_code_rotate": true,
	}
	switch {
	case critical[action]:
		return "critical"
	case warning[action]:
		return "warning"
	default:
		return "info"
	}
}

type securityEvent struct {
	ID        int64  `json:"id"`
	Action    string `json:"action"`
	Severity  string `json:"severity"`
	UserID    string `json:"user_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	Detail    string `json:"detail,omitempty"`
	CreatedAt string `json:"created_at"`
}

// AdminSecuritySummary powers the dashboard security card.
// GET /api/v1/admin/security-summary
func (a *API) AdminSecuritySummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	db := a.store.DB()
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour).Format(time.RFC3339Nano)

	var critical24h, failedLogins24h int
	_ = db.QueryRow(`SELECT COUNT(*) FROM security_audit WHERE created_at >= ? AND action IN ('device_revoked','session_revoked','admin_session_revoked','admin_user_status_changed','recovery_code_disable')`, dayAgo).Scan(&critical24h)
	_ = db.QueryRow(`SELECT COUNT(*) FROM security_audit WHERE created_at >= ? AND action='login_failed'`, dayAgo).Scan(&failedLogins24h)

	var lockedUsers, usersWithFailures int
	_ = db.QueryRow(`SELECT COUNT(*) FROM users WHERE locked_until IS NOT NULL AND locked_until > ?`, now.Format(time.RFC3339Nano)).Scan(&lockedUsers)
	_ = db.QueryRow(`SELECT COUNT(*) FROM users WHERE failed_login_count > 0`).Scan(&usersWithFailures)

	var revokedDevices, activeSessions, adminSessions, pendingRecoveries int
	_ = db.QueryRow(`SELECT COUNT(*) FROM devices WHERE NOT (COALESCE(status,'active')='active')`).Scan(&revokedDevices)
	_ = db.QueryRow(`SELECT COUNT(*) FROM auth_sessions WHERE revoked_at IS NULL AND expires_at > ?`, now.Format(time.RFC3339Nano)).Scan(&activeSessions)
	_ = db.QueryRow(`SELECT COUNT(*) FROM admin_sessions WHERE revoked_at IS NULL AND expires_at > ?`, now.Format(time.RFC3339Nano)).Scan(&adminSessions)
	_ = db.QueryRow(`SELECT COUNT(*) FROM account_recovery_requests WHERE status='pending' AND expires_at > ?`, now.Format(time.RFC3339Nano)).Scan(&pendingRecoveries)

	events := []securityEvent{}
	rows, err := db.Query(`SELECT id, action, user_id, device_id, detail, created_at FROM security_audit ORDER BY created_at DESC, id DESC LIMIT 20`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var e securityEvent
			if err := rows.Scan(&e.ID, &e.Action, &e.UserID, &e.DeviceID, &e.Detail, &e.CreatedAt); err != nil {
				break
			}
			e.Severity = securitySeverity(e.Action)
			events = append(events, e)
		}
	}

	adminActions := []map[string]any{}
	actionRows, err := db.Query(`SELECT id, admin_id, action, target_id, detail, created_at FROM admin_audit ORDER BY created_at DESC, id DESC LIMIT 10`)
	if err == nil {
		defer actionRows.Close()
		for actionRows.Next() {
			var id int64
			var adminID, action, targetID, detail, createdAt string
			if err := actionRows.Scan(&id, &adminID, &action, &targetID, &detail, &createdAt); err != nil {
				break
			}
			adminActions = append(adminActions, map[string]any{
				"id": id, "admin_id": adminID, "action": action,
				"target_id": targetID, "detail": detail, "created_at": createdAt,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"generated_at": now.Format(time.RFC3339),
		"counts": map[string]any{
			"critical_alerts_24h": critical24h,
			"failed_logins_24h":   failedLogins24h,
			"locked_users":        lockedUsers,
			"users_with_failures": usersWithFailures,
			"revoked_devices":     revokedDevices,
			"active_sessions":     activeSessions,
			"admin_sessions":      adminSessions,
			"pending_recoveries":  pendingRecoveries,
		},
		"recent_events":        events,
		"recent_admin_actions": adminActions,
	})
}

// ---------------------------------------------------------------------------
// Activity feed
// ---------------------------------------------------------------------------

type activityEvent struct {
	Type      string `json:"type"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
	TargetID  string `json:"target_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

// severityForActivity derives severity from the event source rather than from
// an invented score: security actions reuse the audited classification and
// destructive admin actions are warnings.
func severityForActivity(kind, action string) string {
	switch kind {
	case "security_event":
		return securitySeverity(action)
	case "admin_action":
		switch action {
		case "revoke_account_device", "crypto_backup_delete", "recovery_code_disable", "account_recovery_reject":
			return "warning"
		default:
			return "info"
		}
	case "recovery_request":
		return "warning"
	default:
		return "info"
	}
}

func activityTitle(kind, action, fallback string) string {
	switch kind {
	case "user_registered":
		return "User registered"
	case "device_registered":
		return "Device registered"
	case "device_revoked":
		return "Device revoked"
	case "file_uploaded":
		return "File uploaded"
	case "call_started":
		return "Call started"
	case "group_created":
		return "Group created"
	case "recovery_request":
		return "Account recovery requested"
	case "admin_action":
		return "Admin action: " + prettyAction(action)
	case "security_event":
		return prettyAction(action)
	default:
		if fallback != "" {
			return fallback
		}
		return kind
	}
}

func prettyAction(action string) string {
	if action == "" {
		return "event"
	}
	replaced := strings.ReplaceAll(action, "_", " ")
	return strings.ToUpper(replaced[:1]) + replaced[1:]
}

// AdminActivity returns a merged, newest-first operational timeline.
// GET /api/v1/admin/activity?limit=40
func (a *API) AdminActivity(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	limit := 40
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			if v < 1 {
				v = 1
			}
			if v > 200 {
				v = 200
			}
			limit = v
		}
	}

	query := `
		SELECT kind, action, title, detail, target_id, created_at FROM (
			SELECT 'user_registered' AS kind, '' AS action, username AS title, '' AS detail, id AS target_id, created_at FROM users
			UNION ALL
			SELECT 'device_registered', '', name || ' · ' || platform, '', id, created_at FROM devices WHERE revoked_at IS NULL
			UNION ALL
			SELECT 'device_revoked', 'device_revoked', name, COALESCE(revoked_at,''), id, revoked_at FROM devices WHERE revoked_at IS NOT NULL
			UNION ALL
			SELECT 'file_uploaded', '', original_name, '', id, created_at FROM files
			UNION ALL
			SELECT 'call_started', '', 'Call ' || status, '', id, started_at FROM calls
			UNION ALL
			SELECT 'group_created', '', name, '', id, created_at FROM groups
			UNION ALL
			SELECT 'recovery_request', 'account_recovery_request', 'Account recovery requested', '', id, created_at FROM account_recovery_requests
			UNION ALL
			SELECT 'security_event', action, '', detail, device_id, created_at FROM security_audit
			UNION ALL
			SELECT 'admin_action', action, '', detail, target_id, created_at FROM admin_audit
		)
		WHERE created_at <> ''
		ORDER BY created_at DESC, kind ASC
		LIMIT ?`

	rows, err := a.store.DB().Query(query, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "activity query failed")
		return
	}
	defer rows.Close()

	events := []activityEvent{}
	for rows.Next() {
		var kind, action, title, detail, targetID, createdAt string
		if err := rows.Scan(&kind, &action, &title, &detail, &targetID, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "activity scan failed")
			return
		}
		events = append(events, activityEvent{
			Type:      kind,
			Severity:  severityForActivity(kind, action),
			Title:     activityTitle(kind, action, title),
			Detail:    strings.TrimSpace(detail),
			TargetID:  targetID,
			CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "activity read failed")
		return
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].CreatedAt > events[j].CreatedAt
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"events":       events,
	})
}
