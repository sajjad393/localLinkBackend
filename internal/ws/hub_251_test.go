package ws

import (
	"net"
	"testing"
)

// Bundle 251: a full queue drops the voice piece but keeps the socket.
func TestTrySendToNeverClosesOnAFullQueue(t *testing.T) {
	server, other := net.Pipe()
	defer other.Close()
	h := NewHub()
	c := NewClient("dev", server)
	h.Add("dev", c)
	// Nobody reads `other`, so the queue fills up.
	took := 0
	for i := 0; i < clientQueueCapacity+50; i++ {
		if h.TrySendTo("dev", []byte(`{"type":"call_voice"}`)) {
			took++
		}
	}
	if took == 0 || took > clientQueueCapacity+2 {
		t.Fatalf("took %d", took)
	}
	if !h.HasDevice("dev") {
		t.Fatal("the socket must stay after a full queue")
	}
	if h.TrySendTo("nobody", []byte("x")) {
		t.Fatal("no socket = not sent")
	}
	c.Close()
}
