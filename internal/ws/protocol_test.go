package ws

import (
	"bufio"
	"bytes"
	"testing"
)

func TestReadFrameRejectsFragmentedFrame(t *testing.T) {
	var raw bytes.Buffer
	raw.Write([]byte{0x01, 0x80, 0, 0, 0, 0})
	_, _, err := ReadFrame(bufio.NewReader(&raw))
	if err == nil {
		t.Fatal("expected fragmented frame rejection")
	}
}
