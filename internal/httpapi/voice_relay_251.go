package httpapi

// Bundle 251 (P9): online voice calls through the server.
//
// The two phones of a CONNECTED call may send `call_voice` frames over their
// WebSocket. Each frame is a voice piece already locked on the phone with the
// per-call key (bundle 159, AES-256-GCM), base64 in `voice`. The server never
// opens it: it only checks the call, the size and the speed, then passes it
// to the other phone. A slow phone loses pieces; its socket is never closed
// for voice.
//
// Limits (per phone): 1 relayed call at a time, 80 pieces a second, 1400
// base64 chars a piece. Server: LOCALLINK_VOICE_RELAY_MAX calls at once
// (default 20); LOCALLINK_VOICE_RELAY=off turns it off.

import (
	"encoding/base64"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	voiceRelayMaxB64251     = 1400
	voiceRelayPerSecond251  = 80
	voiceRelayCheckEvery251 = 5 * time.Second
	voiceRelayIdle251       = 15 * time.Second
	voiceRelayLogEvery251   = 30 * time.Second
	voiceRelayRefuseGap251  = 10 * time.Second
	voiceRelayDefaultMax251 = 20
)

type voiceCall251 struct {
	caller, callee string
	status         string
	checked        time.Time
	lastAt         time.Time
	loggedAt       time.Time
	passed         int
	dropped        int
}

type voiceRate251 struct {
	second time.Time
	count  int
	callID string
	lastAt time.Time
}

type voiceRelay251 struct {
	mu      sync.Mutex
	calls   map[string]*voiceCall251
	phones  map[string]*voiceRate251
	refused map[string]time.Time
}

var voiceRelays251 sync.Map // *API -> *voiceRelay251 (no new API fields)

func (a *API) voiceRelay251() *voiceRelay251 {
	if v, ok := voiceRelays251.Load(a); ok {
		return v.(*voiceRelay251)
	}
	v, _ := voiceRelays251.LoadOrStore(a, &voiceRelay251{
		calls:   map[string]*voiceCall251{},
		phones:  map[string]*voiceRate251{},
		refused: map[string]time.Time{},
	})
	return v.(*voiceRelay251)
}

func voiceRelayOn251() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("LOCALLINK_VOICE_RELAY")))
	return v != "off" && v != "false" && v != "0"
}

func voiceRelayMax251() int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("LOCALLINK_VOICE_RELAY_MAX"))); err == nil && n > 0 {
		return n
	}
	return voiceRelayDefaultMax251
}

func validVoice251(b64 string) bool {
	if b64 == "" || len(b64) > voiceRelayMaxB64251 {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	// nonce 12 + tag 16 + at least 1 byte of voice
	return err == nil && len(raw) >= 29
}

// relayVoice251 decides one frame. peer = the phone to pass it to; reason is
// set when it is refused ("" = ok). sayStop = tell the sender once.
func (a *API) relayVoice251(deviceID, callID, voice string, now time.Time) (peer, reason string, sayStop bool) {
	callID = strings.TrimSpace(callID)
	if !voiceRelayOn251() {
		return "", "relay_off", a.voiceRefuse251(deviceID, callID, now)
	}
	if callID == "" || !validVoice251(voice) {
		return "", "bad_frame", false
	}
	r := a.voiceRelay251()
	r.mu.Lock()
	r.evict(now)
	ph := r.phones[deviceID]
	if ph == nil {
		ph = &voiceRate251{}
		r.phones[deviceID] = ph
	}
	if ph.callID != "" && ph.callID != callID && now.Sub(ph.lastAt) < voiceRelayIdle251 {
		r.mu.Unlock()
		return "", "one_call_only", a.voiceRefuse251(deviceID, callID, now)
	}
	if now.Sub(ph.second) >= time.Second {
		ph.second, ph.count = now, 0
	}
	ph.count++
	ph.callID, ph.lastAt = callID, now
	c := r.calls[callID]
	if c != nil && ph.count > voiceRelayPerSecond251 {
		c.dropped++
		r.mu.Unlock()
		return "", "too_fast", false
	}
	if c == nil && len(r.calls) >= voiceRelayMax251() {
		r.mu.Unlock()
		return "", "server_full", a.voiceRefuse251(deviceID, callID, now)
	}
	needCheck := c == nil || now.Sub(c.checked) >= voiceRelayCheckEvery251
	r.mu.Unlock()

	if needCheck {
		// No DB call under the relay lock (one DB connection).
		call, err := a.callForParticipant(callID, deviceID)
		if err != nil {
			return "", "not_in_call", a.voiceRefuse251(deviceID, callID, now)
		}
		r.mu.Lock()
		c = r.calls[callID]
		if c == nil {
			c = &voiceCall251{loggedAt: now}
			r.calls[callID] = c
			log.Printf("VOICE_RELAY_251 at=start call=%s", shortID251(callID))
		}
		c.caller, c.callee, c.status, c.checked = call.CallerID, call.CalleeID, call.Status, now
		r.mu.Unlock()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	c = r.calls[callID]
	if c == nil || (deviceID != c.caller && deviceID != c.callee) {
		return "", "not_in_call", false
	}
	if c.status != "connected" {
		c.dropped++
		if c.status == "ringing" {
			return "", "not_answered", false
		}
		delete(r.calls, callID)
		return "", "call_over", true
	}
	c.passed++
	c.lastAt = now
	if now.Sub(c.loggedAt) >= voiceRelayLogEvery251 {
		c.loggedAt = now
		log.Printf("VOICE_RELAY_251 at=run call=%s passed=%d dropped=%d", shortID251(callID), c.passed, c.dropped)
	}
	if deviceID == c.caller {
		return c.callee, "", false
	}
	return c.caller, "", false
}

// noteVoiceDropped251: the other phone's queue was full.
func (a *API) noteVoiceDropped251(callID string) {
	r := a.voiceRelay251()
	r.mu.Lock()
	if c := r.calls[strings.TrimSpace(callID)]; c != nil {
		c.dropped++
		if c.passed > 0 {
			c.passed--
		}
	}
	r.mu.Unlock()
}

// voiceRefuse251: true at most once per phone+call every 10 s.
func (a *API) voiceRefuse251(deviceID, callID string, now time.Time) bool {
	r := a.voiceRelay251()
	r.mu.Lock()
	defer r.mu.Unlock()
	k := deviceID + "|" + callID
	if t, ok := r.refused[k]; ok && now.Sub(t) < voiceRelayRefuseGap251 {
		return false
	}
	if len(r.refused) > 1000 {
		r.refused = map[string]time.Time{}
	}
	r.refused[k] = now
	return true
}

// evict forgets quiet calls and phones. Caller holds r.mu.
func (r *voiceRelay251) evict(now time.Time) {
	for id, c := range r.calls {
		last := c.lastAt
		if last.IsZero() {
			last = c.checked
		}
		if now.Sub(last) >= voiceRelayIdle251 {
			log.Printf("VOICE_RELAY_251 at=end call=%s passed=%d dropped=%d", shortID251(id), c.passed, c.dropped)
			delete(r.calls, id)
		}
	}
	for id, p := range r.phones {
		if now.Sub(p.lastAt) >= voiceRelayIdle251 {
			delete(r.phones, id)
		}
	}
}

func (a *API) voiceRelayCalls251() int {
	r := a.voiceRelay251()
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func shortID251(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[len(id)-6:]
}
