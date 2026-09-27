package ws

import (
	"bufio"
	"bytes"
	"net"
	"testing"
)

func TestReadFrameRejectsUnmaskedClientFrame(t *testing.T) {
	var raw bytes.Buffer
	raw.Write([]byte{0x81, 0x02})
	raw.WriteString("ok")
	_, _, err := ReadFrame(bufio.NewReader(&raw))
	if err == nil {
		t.Fatal("expected unmasked frame to be rejected")
	}
}

func TestReadFrameRejectsOversizedFrame(t *testing.T) {
	var raw bytes.Buffer
	raw.Write([]byte{0x81, 0xFE, 0xFF, 0xFF, 0, 0, 0, 0})
	_, _, err := ReadFrame(bufio.NewReader(&raw))
	if err == nil {
		t.Fatal("expected oversized frame to be rejected")
	}
}

func TestClientOutboundQueueIsBounded(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()
	c := NewClient("device", c1)
	defer c.Close()
	for i := 0; i < clientQueueCapacity; i++ {
		if err := c.EnqueueText([]byte("x")); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if err := c.EnqueueText([]byte("overflow")); err != ErrClientQueueFull {
		t.Fatalf("expected queue-full error, got %v", err)
	}
}

func TestHubCloseDevice(t *testing.T) {
	h := NewHub()
	server, client := net.Pipe()
	c := NewClient("device-a", server)
	h.Add("device-a", c)
	h.CloseDevice("device-a")
	if h.HasDevice("device-a") {
		t.Fatal("device should have been removed")
	}
	_ = client.Close()
}
