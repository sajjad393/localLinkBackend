package main

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type runtimeMetrics struct {
	startedAt  time.Time
	requests   atomic.Uint64
	successes  atomic.Uint64
	clientErrs atomic.Uint64
	serverErrs atomic.Uint64
	panics     atomic.Uint64
	active     atomic.Int64
	bytesOut   atomic.Uint64
	buckets    *hourlyBuckets
}

func newRuntimeMetrics() *runtimeMetrics {
	return &runtimeMetrics{startedAt: time.Now().UTC(), buckets: newHourlyBuckets()}
}

// hourlyBuckets keeps 90 days of hourly request/error/byte counters so the
// admin dashboard can chart error-rate and traffic trends from real traffic.
// Response bytes are counted for HTTP responses; hijacked WebSocket
// connections are not included.
type hourlyBuckets struct {
	mu     sync.Mutex
	values map[int64]bucketCounters
	pruned time.Time
}

type bucketCounters struct {
	Requests     uint64
	ClientErrors uint64
	ServerErrors uint64
	BytesOut     uint64
}

const bucketRetention = 90 * 24 * time.Hour

func newHourlyBuckets() *hourlyBuckets {
	return &hourlyBuckets{values: make(map[int64]bucketCounters)}
}

func (b *hourlyBuckets) record(when time.Time, status int, bytes int64) {
	slot := when.UTC().Truncate(time.Hour).Unix()
	b.mu.Lock()
	defer b.mu.Unlock()
	counters := b.values[slot]
	counters.Requests++
	switch {
	case status >= 500:
		counters.ServerErrors++
	case status >= 400:
		counters.ClientErrors++
	}
	if bytes > 0 {
		counters.BytesOut += uint64(bytes)
	}
	b.values[slot] = counters
	if when.Sub(b.pruned) > time.Hour {
		b.pruned = when
		cutoff := when.UTC().Add(-bucketRetention).Unix()
		for key := range b.values {
			if key < cutoff {
				delete(b.values, key)
			}
		}
	}
}

func (b *hourlyBuckets) snapshot() []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	keys := make([]int64, 0, len(b.values))
	for key := range b.values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		counters := b.values[key]
		out = append(out, map[string]any{
			"t":             time.Unix(key, 0).UTC().Format(time.RFC3339),
			"requests":      counters.Requests,
			"client_errors": counters.ClientErrors,
			"server_errors": counters.ServerErrors,
			"errors":        counters.ClientErrors + counters.ServerErrors,
			"bytes_out":     counters.BytesOut,
		})
	}
	return out
}

type statusRecordingWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
	bytes  int64
}

func (w *statusRecordingWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecordingWriter) Write(body []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += int64(n)
	return n, err
}

func (w *statusRecordingWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusRecordingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hj.Hijack()
}

func (w *statusRecordingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func requestLogging(metrics *runtimeMetrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		recorder := &statusRecordingWriter{ResponseWriter: w}
		metrics.requests.Add(1)
		metrics.active.Add(1)
		defer metrics.active.Add(-1)

		start := time.Now()
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if !recorder.wrote {
			status = http.StatusOK
		}
		switch {
		case status >= 500:
			metrics.serverErrs.Add(1)
		case status >= 400:
			metrics.clientErrs.Add(1)
		case status >= 200:
			metrics.successes.Add(1)
		}
		metrics.bytesOut.Add(uint64(recorder.bytes))
		metrics.buckets.record(time.Now().UTC(), status, recorder.bytes)
		log.Printf("request id=%s method=%s path=%s status=%d duration=%s", requestID, r.Method, r.URL.Path, status, time.Since(start))
	})
}

func requestRecovery(metrics *runtimeMetrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				metrics.panics.Add(1)
				requestID := w.Header().Get("X-Request-ID")
				log.Printf("panic recovered id=%s path=%s value_type=%T", requestID, r.URL.Path, recovered)
				log.Printf("panic stack id=%s stack=%q", requestID, debug.Stack())
				if !hasWebSocketUpgrade(r) {
					if recorder, ok := w.(*statusRecordingWriter); ok && !recorder.wrote {
						writeRuntimeError(recorder, http.StatusInternalServerError, requestID)
					} else if _, ok := w.(*statusRecordingWriter); !ok {
						writeRuntimeError(w, http.StatusInternalServerError, requestID)
					}
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err == nil {
		return hex.EncodeToString(raw)
	}
	fallback := make([]byte, 8)
	binary.BigEndian.PutUint64(fallback, uint64(time.Now().UnixNano()))
	return hex.EncodeToString(fallback)
}

func writeRuntimeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeRuntimeError(w http.ResponseWriter, status int, requestID string) {
	writeRuntimeJSON(w, status, map[string]any{
		"ok":         false,
		"error":      "internal server error",
		"request_id": requestID,
	})
}

func (m *runtimeMetrics) snapshot() map[string]any {
	return map[string]any{
		"schema":           1,
		"generated_at":     time.Now().UTC().Format(time.RFC3339Nano),
		"started_at":       m.startedAt.Format(time.RFC3339Nano),
		"uptime_seconds":   int64(time.Since(m.startedAt).Seconds()),
		"requests":         m.requests.Load(),
		"successes":        m.successes.Load(),
		"client_errors":    m.clientErrs.Load(),
		"server_errors":    m.serverErrs.Load(),
		"recovered_panics": m.panics.Load(),
		"active_requests":  m.active.Load(),
		"bytes_out":        m.bytesOut.Load(),
		"buckets":          m.buckets.snapshot(),
	}
}
