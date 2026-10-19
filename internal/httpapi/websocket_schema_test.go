package httpapi

import (
	"encoding/json"
	"testing"
)

// Call-relay wire contract: the app-side WS fallback (LocalLink bundle 51)
// depends on these fields surviving the backend relay verbatim.
func TestCallRelaySchemaRoundTrip(t *testing.T) {
	enabled := true
	in := WSMessage{
		Type:                 "call_invite",
		CallID:               "call-x",
		SenderID:             "caller-1",
		RecipientID:          "callee-1",
		Status:               "ringing",
		TransportMode:        "offline_mesh",
		SupportedMediaCodecs: []string{"opus"},
		Enabled:              &enabled,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out WSMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.TransportMode != "offline_mesh" {
		t.Fatalf("transport_mode lost: got %q", out.TransportMode)
	}
	if len(out.SupportedMediaCodecs) != 1 || out.SupportedMediaCodecs[0] != "opus" {
		t.Fatalf("supported_media_codecs lost: got %v", out.SupportedMediaCodecs)
	}
	if out.Enabled == nil || *out.Enabled != true {
		t.Fatalf("enabled flag lost: got %v", out.Enabled)
	}
	if out.CallID != "call-x" || out.SenderID != "caller-1" || out.RecipientID != "callee-1" {
		t.Fatalf("identity fields mutated: %+v", out)
	}
}

func TestCallPeerVideoStateSchemaName(t *testing.T) {
	disabled := false
	raw, err := json.Marshal(WSMessage{Type: "call_peer_video_state", CallID: "c", Enabled: &disabled})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The app listenes for "enabled" (as in the mesh protocol); if the JSON
	// key drifts the video toggle silently never reaches the peer's UI.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v, ok := m["enabled"]
	if !ok {
		t.Fatalf("json key must be 'enabled', got map %v", m)
	}
	if b, _ := v.(bool); b != false {
		t.Fatalf("expected false enabled value, got %v", v)
	}
}
