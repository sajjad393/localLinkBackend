package httpapi

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func voiceFrame251() string { return base64.StdEncoding.EncodeToString(make([]byte, 60)) }

func insertCall251(t *testing.T, a *API, id, caller, callee, status string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, d := range []string{caller, callee} {
		if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO devices(id,name,platform,created_at,last_seen_at,status) VALUES(?,?,?,?,?,'active')`, d, d, "android", now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.store.DB().Exec(`INSERT INTO calls(id,caller_id,callee_id,status,started_at,duration_seconds,end_reason,last_activity_at) VALUES(?,?,?,?,?,0,'',?)`,
		id, caller, callee, status, now, now); err != nil {
		t.Fatal(err)
	}
}

func setCallStatus251(t *testing.T, a *API, id, status string) {
	t.Helper()
	if _, err := a.store.DB().Exec(`UPDATE calls SET status=? WHERE id=?`, status, id); err != nil {
		t.Fatal(err)
	}
}

func TestVoiceRelay251PassesBothWaysOnlyInAConnectedCall(t *testing.T) {
	_, a := newTestAPI(t)
	insertCall251(t, a, "call-1", "devA", "devB", "ringing")
	now := time.Now()
	if peer, reason, _ := a.relayVoice251("devA", "call-1", voiceFrame251(), now); peer != "" || reason != "not_answered" {
		t.Fatalf("ringing: peer=%q reason=%q", peer, reason)
	}
	setCallStatus251(t, a, "call-1", "connected")
	now = now.Add(voiceRelayCheckEvery251)
	if peer, reason, _ := a.relayVoice251("devA", "call-1", voiceFrame251(), now); peer != "devB" || reason != "" {
		t.Fatalf("A->B: peer=%q reason=%q", peer, reason)
	}
	if peer, _, _ := a.relayVoice251("devB", "call-1", voiceFrame251(), now); peer != "devA" {
		t.Fatalf("B->A: peer=%q", peer)
	}
	if peer, reason, stop := a.relayVoice251("devC", "call-1", voiceFrame251(), now); peer != "" || reason != "not_in_call" || stop {
		t.Fatalf("stranger: peer=%q reason=%q stop=%v", peer, reason, stop)
	}
	// Ended call: the next check stops the relay and tells the phone once.
	setCallStatus251(t, a, "call-1", "ended")
	now = now.Add(voiceRelayCheckEvery251)
	if peer, reason, stop := a.relayVoice251("devA", "call-1", voiceFrame251(), now); peer != "" || reason != "call_over" || !stop {
		t.Fatalf("ended: peer=%q reason=%q stop=%v", peer, reason, stop)
	}
}

func TestVoiceRelay251RefusesBadFramesAndUnknownCalls(t *testing.T) {
	_, a := newTestAPI(t)
	now := time.Now()
	for _, v := range []string{"", "!!notbase64!!", base64.StdEncoding.EncodeToString(make([]byte, 10)), strings.Repeat("A", voiceRelayMaxB64251+4)} {
		if peer, reason, _ := a.relayVoice251("devA", "call-x", v, now); peer != "" || reason != "bad_frame" {
			t.Fatalf("frame %q: peer=%q reason=%q", v[:min(len(v), 10)], peer, reason)
		}
	}
	_, reason, stop := a.relayVoice251("devA", "call-x", voiceFrame251(), now)
	if reason != "not_in_call" || !stop {
		t.Fatalf("unknown call: reason=%q stop=%v", reason, stop)
	}
	if _, _, stop := a.relayVoice251("devA", "call-x", voiceFrame251(), now.Add(time.Second)); stop {
		t.Fatal("the stop note must not repeat within 10 s")
	}
}

func TestVoiceRelay251SpeedAndOneCallLimits(t *testing.T) {
	_, a := newTestAPI(t)
	insertCall251(t, a, "call-1", "devA", "devB", "connected")
	insertCall251(t, a, "call-2", "devA", "devC", "connected")
	now := time.Now()
	passed := 0
	for i := 0; i < voiceRelayPerSecond251+20; i++ {
		if peer, _, _ := a.relayVoice251("devA", "call-1", voiceFrame251(), now); peer != "" {
			passed++
		}
	}
	if passed != voiceRelayPerSecond251 {
		t.Fatalf("passed %d in one second, want %d", passed, voiceRelayPerSecond251)
	}
	if _, reason, _ := a.relayVoice251("devA", "call-2", voiceFrame251(), now.Add(time.Second)); reason != "one_call_only" {
		t.Fatalf("second call: %q", reason)
	}
	// After 15 s quiet the phone may use the relay for another call.
	if peer, _, _ := a.relayVoice251("devA", "call-2", voiceFrame251(), now.Add(voiceRelayIdle251+time.Second)); peer != "devC" {
		t.Fatalf("after idle: peer=%q", peer)
	}
	if n := a.voiceRelayCalls251(); n != 1 {
		t.Fatalf("calls in relay = %d, want 1 (quiet call forgotten)", n)
	}
}

func TestVoiceRelay251ServerCapAndOffSwitch(t *testing.T) {
	_, a := newTestAPI(t)
	t.Setenv("LOCALLINK_VOICE_RELAY_MAX", "1")
	insertCall251(t, a, "call-1", "devA", "devB", "connected")
	insertCall251(t, a, "call-2", "devC", "devD", "connected")
	now := time.Now()
	if peer, _, _ := a.relayVoice251("devA", "call-1", voiceFrame251(), now); peer != "devB" {
		t.Fatalf("first call: %q", peer)
	}
	if _, reason, stop := a.relayVoice251("devC", "call-2", voiceFrame251(), now); reason != "server_full" || !stop {
		t.Fatalf("cap: reason=%q stop=%v", reason, stop)
	}
	t.Setenv("LOCALLINK_VOICE_RELAY", "off")
	if _, reason, _ := a.relayVoice251("devA", "call-1", voiceFrame251(), now); reason != "relay_off" {
		t.Fatalf("off: %q", reason)
	}
}
