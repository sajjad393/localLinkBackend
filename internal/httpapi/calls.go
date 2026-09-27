package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CallRecord struct {
	ID              string `json:"id"`
	CallerID        string `json:"caller_id"`
	CalleeID        string `json:"callee_id"`
	Status          string `json:"status"`
	StartedAt       string `json:"started_at"`
	AnsweredAt      string `json:"answered_at,omitempty"`
	EndedAt         string `json:"ended_at,omitempty"`
	DurationSeconds int64  `json:"duration_seconds"`
	EndReason       string `json:"end_reason,omitempty"`
	LastActivityAt  string `json:"last_activity_at,omitempty"`
}

func (a *API) ListCalls(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	params, err := parsePageParams(r, "calls:"+deviceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{deviceID, deviceID}
	where := `(caller_id=? OR callee_id=?)`
	if params.Cursor != nil {
		where += ` AND (started_at < ? OR (started_at=? AND id<?))`
		args = append(args, params.Cursor.Key, params.Cursor.Key, params.Cursor.ID)
	}
	args = append(args, params.Limit+1)
	rows, err := a.store.DB().Query(`SELECT id,caller_id,callee_id,status,started_at,COALESCE(answered_at,''),COALESCE(ended_at,''),duration_seconds,COALESCE(end_reason,''),COALESCE(last_activity_at,started_at) FROM calls WHERE `+where+` ORDER BY started_at DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list calls")
		return
	}
	defer rows.Close()
	out := make([]CallRecord, 0, params.Limit)
	for rows.Next() {
		var call CallRecord
		if err := rows.Scan(&call.ID, &call.CallerID, &call.CalleeID, &call.Status, &call.StartedAt, &call.AnsweredAt, &call.EndedAt, &call.DurationSeconds, &call.EndReason, &call.LastActivityAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read calls")
			return
		}
		out = append(out, call)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read calls")
		return
	}
	hasMore := len(out) > params.Limit
	if hasMore {
		out = out[:params.Limit]
	}
	resp := struct {
		Calls []CallRecord `json:"calls"`
		PageInfo
	}{Calls: out}
	resp.HasMore = hasMore
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		resp.NextCursor = encodeListCursor("calls:"+deviceID, last.StartedAt, last.ID, true)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) ReconcileCall(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in CallRecord
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid call record")
		return
	}
	in.ID = strings.TrimSpace(in.ID)
	in.CallerID = strings.TrimSpace(in.CallerID)
	in.CalleeID = strings.TrimSpace(in.CalleeID)
	in.Status = strings.TrimSpace(in.Status)
	in.StartedAt = strings.TrimSpace(in.StartedAt)
	in.AnsweredAt = strings.TrimSpace(in.AnsweredAt)
	in.EndedAt = strings.TrimSpace(in.EndedAt)
	in.EndReason = strings.TrimSpace(in.EndReason)
	if in.ID == "" || len(in.ID) > 128 || hasControlChars(in.ID) || in.CallerID == "" || in.CalleeID == "" || in.CallerID == in.CalleeID || (deviceID != in.CallerID && deviceID != in.CalleeID) {
		writeError(w, http.StatusBadRequest, "invalid call participants or call id")
		return
	}
	if !a.deviceExists(in.CallerID) || !a.deviceExists(in.CalleeID) {
		writeError(w, http.StatusBadRequest, "call participant does not exist")
		return
	}
	switch in.Status {
	case "ringing", "connected", "rejected", "ended", "missed", "canceled", "failed":
	default:
		writeError(w, http.StatusBadRequest, "invalid call status")
		return
	}
	if in.StartedAt == "" {
		in.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if in.LastActivityAt == "" {
		in.LastActivityAt = in.EndedAt
		if in.LastActivityAt == "" {
			in.LastActivityAt = in.AnsweredAt
		}
		if in.LastActivityAt == "" {
			in.LastActivityAt = in.StartedAt
		}
	}

	current, err := a.callForParticipant(in.ID, deviceID)
	if err != nil {
		if err.Error() != "call not found" {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		_, err := a.store.DB().Exec(`INSERT INTO calls(id,caller_id,callee_id,status,started_at,answered_at,ended_at,duration_seconds,end_reason,last_activity_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			in.ID, in.CallerID, in.CalleeID, in.Status, in.StartedAt, nullableString(in.AnsweredAt), nullableString(in.EndedAt), maxInt64(in.DurationSeconds, 0), in.EndReason, nullableString(in.LastActivityAt))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save reconciled call")
			return
		}
		writeJSON(w, http.StatusCreated, in)
		return
	}
	if current.CallerID != in.CallerID || current.CalleeID != in.CalleeID {
		writeError(w, http.StatusConflict, "call participants do not match existing call")
		return
	}
	merged := mergeCallRecords(current, in)
	if _, err := a.store.DB().Exec(`UPDATE calls SET status=?,answered_at=?,ended_at=?,duration_seconds=?,end_reason=?,last_activity_at=? WHERE id=?`,
		merged.Status, nullableString(merged.AnsweredAt), nullableString(merged.EndedAt), maxInt64(merged.DurationSeconds, 0), merged.EndReason, nullableString(merged.LastActivityAt), merged.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reconcile call")
		return
	}
	writeJSON(w, http.StatusOK, merged)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func mergeCallRecords(current, incoming CallRecord) CallRecord {
	terminal := func(status string) bool {
		switch status {
		case "rejected", "ended", "missed", "canceled", "failed":
			return true
		default:
			return false
		}
	}
	out := current
	if terminal(incoming.Status) || (!terminal(current.Status) && incoming.Status == "connected") || current.Status == "ringing" && incoming.Status == "ringing" {
		out.Status = incoming.Status
	}
	if out.StartedAt == "" || (incoming.StartedAt != "" && incoming.StartedAt < out.StartedAt) {
		out.StartedAt = incoming.StartedAt
	}
	if incoming.AnsweredAt != "" && (out.AnsweredAt == "" || incoming.AnsweredAt < out.AnsweredAt) {
		out.AnsweredAt = incoming.AnsweredAt
	}
	if incoming.EndedAt != "" && (out.EndedAt == "" || incoming.EndedAt > out.EndedAt) {
		out.EndedAt = incoming.EndedAt
	}
	out.DurationSeconds = maxInt64(out.DurationSeconds, incoming.DurationSeconds)
	if incoming.EndReason != "" && (out.EndReason == "" || terminal(incoming.Status)) {
		out.EndReason = incoming.EndReason
	}
	if incoming.LastActivityAt > out.LastActivityAt {
		out.LastActivityAt = incoming.LastActivityAt
	}
	return out
}

func (a *API) createCall(callerID, calleeID, requestedID string) (CallRecord, bool, error) {
	callerID, calleeID, requestedID = strings.TrimSpace(callerID), strings.TrimSpace(calleeID), strings.TrimSpace(requestedID)
	if callerID == "" || calleeID == "" || callerID == calleeID {
		return CallRecord{}, false, errors.New("invalid call participants")
	}
	if !a.deviceExists(calleeID) {
		return CallRecord{}, false, errors.New("callee device not found")
	}
	if requestedID == "" {
		requestedID = uuid.NewString()
	}
	if len(requestedID) > 128 || hasControlChars(requestedID) {
		return CallRecord{}, false, errors.New("invalid call id")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := a.store.DB().Exec(`
INSERT INTO calls(id,caller_id,callee_id,status,started_at,answered_at,ended_at,duration_seconds,end_reason,last_activity_at)
VALUES(?,?,?,?,?,?,?,?,?,?)`, requestedID, callerID, calleeID, "ringing", now, nil, nil, 0, "", now)
	if err == nil {
		return CallRecord{ID: requestedID, CallerID: callerID, CalleeID: calleeID, Status: "ringing", StartedAt: now}, false, nil
	}
	var call CallRecord
	scanErr := a.store.DB().QueryRow(`
SELECT id,caller_id,callee_id,status,started_at,COALESCE(answered_at,''),COALESCE(ended_at,''),duration_seconds,COALESCE(end_reason,''),COALESCE(last_activity_at,started_at)
FROM calls WHERE id=?`, requestedID).Scan(&call.ID, &call.CallerID, &call.CalleeID, &call.Status, &call.StartedAt, &call.AnsweredAt, &call.EndedAt, &call.DurationSeconds, &call.EndReason, &call.LastActivityAt)
	if scanErr != nil {
		return CallRecord{}, false, err
	}
	if call.CallerID != callerID || call.CalleeID != calleeID {
		return CallRecord{}, false, errors.New("call id is already used by another call")
	}
	return call, true, nil
}

func (a *API) callForParticipant(callID, deviceID string) (CallRecord, error) {
	var call CallRecord
	err := a.store.DB().QueryRow(`
SELECT id,caller_id,callee_id,status,started_at,COALESCE(answered_at,''),COALESCE(ended_at,''),duration_seconds,COALESCE(end_reason,''),COALESCE(last_activity_at,started_at)
FROM calls WHERE id=?`, callID).Scan(&call.ID, &call.CallerID, &call.CalleeID, &call.Status, &call.StartedAt, &call.AnsweredAt, &call.EndedAt, &call.DurationSeconds, &call.EndReason, &call.LastActivityAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CallRecord{}, errors.New("call not found")
		}
		return CallRecord{}, err
	}
	if deviceID != call.CallerID && deviceID != call.CalleeID {
		return CallRecord{}, errors.New("device is not a participant in this call")
	}
	return call, nil
}

func (a *API) updateCallState(callID, actorID, status, reason string) (CallRecord, error) {
	call, err := a.callForParticipant(strings.TrimSpace(callID), strings.TrimSpace(actorID))
	if err != nil {
		return CallRecord{}, err
	}
	status, reason = strings.TrimSpace(status), strings.TrimSpace(reason)
	now := time.Now().UTC()
	answeredAt, endedAt, duration, endReason := call.AnsweredAt, call.EndedAt, call.DurationSeconds, call.EndReason
	switch status {
	case "connected":
		if answeredAt == "" {
			answeredAt = now.Format(time.RFC3339Nano)
		}
	case "rejected", "ended", "failed", "missed", "canceled":
		if endedAt == "" {
			endedAt = now.Format(time.RFC3339Nano)
		}
		if answeredAt != "" {
			if started, parseErr := time.Parse(time.RFC3339Nano, answeredAt); parseErr == nil {
				duration = int64(now.Sub(started).Seconds())
				if duration < 0 {
					duration = 0
				}
			}
		}
		if reason != "" {
			endReason = reason
		}
	default:
		return CallRecord{}, errors.New("invalid call status")
	}
	activity := now.Format(time.RFC3339Nano)
	if _, err := a.store.DB().Exec(`UPDATE calls SET status=?,answered_at=?,ended_at=?,duration_seconds=?,end_reason=?,last_activity_at=? WHERE id=?`,
		status, nullableString(answeredAt), nullableString(endedAt), duration, endReason, activity, call.ID); err != nil {
		return CallRecord{}, err
	}
	call.Status, call.AnsweredAt, call.EndedAt, call.DurationSeconds, call.EndReason, call.LastActivityAt = status, answeredAt, endedAt, duration, endReason, activity
	return call, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func (a *API) TouchCall(callID, actorID string) (CallRecord, error) {
	call, err := a.callForParticipant(strings.TrimSpace(callID), strings.TrimSpace(actorID))
	if err != nil {
		return CallRecord{}, err
	}
	if call.Status != "ringing" && call.Status != "connected" {
		return call, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := a.store.DB().Exec("UPDATE calls SET last_activity_at=? WHERE id=?", now, call.ID); err != nil {
		return CallRecord{}, err
	}
	call.LastActivityAt = now
	return call, nil
}

func (a *API) CleanupStaleCalls(ringingTTL, connectedTTL time.Duration) (int, error) {
	now := time.Now().UTC()
	total := 0
	nowText := now.Format(time.RFC3339Nano)
	ringingCutoff := now.Add(-ringingTTL).Format(time.RFC3339Nano)
	result, err := a.store.DB().Exec(`UPDATE calls SET status='missed',ended_at=?,end_reason='server_timeout',last_activity_at=? WHERE status='ringing' AND COALESCE(last_activity_at,started_at) < ?`, nowText, nowText, ringingCutoff)
	if err != nil {
		return 0, err
	}
	if n, err := result.RowsAffected(); err == nil {
		total += int(n)
	}
	connectedCutoff := now.Add(-connectedTTL).Format(time.RFC3339Nano)
	rows, err := a.store.DB().Query(`SELECT id,caller_id,callee_id,COALESCE(answered_at,'') FROM calls WHERE status='connected' AND COALESCE(last_activity_at,started_at) < ?`, connectedCutoff)
	if err != nil {
		return total, err
	}
	type staleCall struct{ id, caller, callee, answered string }
	stale := make([]staleCall, 0)
	for rows.Next() {
		var c staleCall
		if err := rows.Scan(&c.id, &c.caller, &c.callee, &c.answered); err != nil {
			rows.Close()
			return total, err
		}
		stale = append(stale, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return total, err
	}
	rows.Close()
	for _, c := range stale {
		duration := int64(0)
		if c.answered != "" {
			if started, parseErr := time.Parse(time.RFC3339Nano, c.answered); parseErr == nil {
				duration = int64(now.Sub(started).Seconds())
				if duration < 0 {
					duration = 0
				}
			}
		}
		result, err := a.store.DB().Exec(`UPDATE calls SET status='failed',ended_at=?,duration_seconds=?,end_reason='heartbeat_timeout',last_activity_at=? WHERE id=? AND status='connected'`, nowText, duration, nowText, c.id)
		if err != nil {
			return total, err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			continue
		}
		total++
		payload := mustJSON(WSMessage{Type: "call_state", CallID: c.id, SenderID: c.caller, RecipientID: c.callee, Status: "failed", CreatedAt: nowText, Reason: "heartbeat_timeout"})
		a.hub.SendTo(c.caller, payload)
		a.hub.SendTo(c.callee, payload)
	}
	return total, nil
}
