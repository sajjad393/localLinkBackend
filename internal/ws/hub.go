package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	websocketGUID            = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	TextFrame           byte = 0x1
	CloseFrame          byte = 0x8
	PingFrame           byte = 0x9
	PongFrame           byte = 0xA
	maxFramePayload          = 64 * 1024
	clientQueueCapacity      = 128
)

var ErrClientQueueFull = errors.New("websocket client outbound queue full")
var ErrClientClosed = errors.New("websocket client closed")

type Client struct {
	id        string
	conn      net.Conn
	mu        sync.Mutex
	outbound  chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*Client]struct{}
}

func NewHub() *Hub { return &Hub{clients: make(map[string]map[*Client]struct{})} }

func (h *Hub) Add(deviceID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[deviceID] == nil {
		h.clients[deviceID] = make(map[*Client]struct{})
	}
	h.clients[deviceID][c] = struct{}{}
}

func (h *Hub) Remove(deviceID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.clients[deviceID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, deviceID)
		}
	}
}

func (h *Hub) SendTo(deviceID string, payload []byte) {
	h.mu.RLock()
	set := h.clients[deviceID]
	clients := make([]*Client, 0, len(set))
	for c := range set {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		if err := c.EnqueueText(payload); err != nil {
			c.Close()
			h.Remove(deviceID, c)
		}
	}
}

func (h *Hub) CloseDevice(deviceID string) {
	h.mu.Lock()
	set := h.clients[deviceID]
	clients := make([]*Client, 0, len(set))
	for c := range set {
		clients = append(clients, c)
	}
	delete(h.clients, deviceID)
	h.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
}

func (h *Hub) HasDevice(deviceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[deviceID]) > 0
}

func (h *Hub) DeviceIDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

func Handshake(conn net.Conn, key string) error {
	h := sha1.Sum([]byte(key + websocketGUID))
	accept := base64(h[:])
	_, err := fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	return err
}

func base64(src []byte) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var b strings.Builder
	for i := 0; i < len(src); i += 3 {
		n := int(src[i]) << 16
		if i+1 < len(src) {
			n |= int(src[i+1]) << 8
		}
		if i+2 < len(src) {
			n |= int(src[i+2])
		}
		b.WriteByte(table[(n>>18)&63])
		b.WriteByte(table[(n>>12)&63])
		if i+1 < len(src) {
			b.WriteByte(table[(n>>6)&63])
		} else {
			b.WriteByte('=')
		}
		if i+2 < len(src) {
			b.WriteByte(table[n&63])
		} else {
			b.WriteByte('=')
		}
	}
	return b.String()
}

func (c *Client) WriteText(payload []byte) error { return c.writeFrame(TextFrame, payload) }
func (c *Client) WritePong(payload []byte) error { return c.writeFrame(PongFrame, payload) }
func (c *Client) WritePing(payload []byte) error { return c.writeFrame(PingFrame, payload) }
func (c *Client) WriteClose() error              { return c.writeFrame(CloseFrame, nil) }

func (c *Client) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	defer c.conn.SetWriteDeadline(time.Time{})
	return writeFrame(c.conn, opcode, payload)
}

func ReadFrame(r *bufio.Reader) (byte, []byte, error) {
	b1, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	b2, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if b1&0x80 == 0 || b1&0x70 != 0 {
		return 0, nil, fmt.Errorf("fragmented or reserved websocket frame")
	}
	opcode := b1 & 0x0F
	masked := b2&0x80 != 0
	n := int64(b2 & 0x7F)
	if n == 126 {
		var x uint16
		if err = binary.Read(r, binary.BigEndian, &x); err != nil {
			return 0, nil, err
		}
		n = int64(x)
	}
	if (opcode == CloseFrame || opcode == PingFrame || opcode == PongFrame) && (b2&0x7F) > 125 {
		return 0, nil, fmt.Errorf("control frame too large")
	}
	if n == 127 {
		var x uint64
		if err = binary.Read(r, binary.BigEndian, &x); err != nil {
			return 0, nil, err
		}
		if x > maxFramePayload {
			return 0, nil, fmt.Errorf("frame too large")
		}
		n = int64(x)
	}
	if n > maxFramePayload {
		return 0, nil, fmt.Errorf("frame too large")
	}
	if !masked {
		return 0, nil, fmt.Errorf("client frame is not masked")
	}
	var mask [4]byte
	if _, err = io.ReadFull(r, mask[:]); err != nil {
		return 0, nil, err
	}
	p := make([]byte, n)
	if _, err = io.ReadFull(r, p); err != nil {
		return 0, nil, err
	}
	for i := range p {
		p[i] ^= mask[i%4]
	}
	return opcode, p, nil
}

func writeFrame(w io.Writer, opcode byte, payload []byte) error {
	var h [10]byte
	h[0] = 0x80 | opcode
	n := len(payload)
	var hn int
	switch {
	case n < 126:
		h[1] = byte(n)
		hn = 2
	case n <= 65535:
		h[1] = 126
		binary.BigEndian.PutUint16(h[2:4], uint16(n))
		hn = 4
	default:
		h[1] = 127
		binary.BigEndian.PutUint64(h[2:10], uint64(n))
		hn = 10
	}
	if _, err := w.Write(h[:hn]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func NewClient(id string, conn net.Conn) *Client {
	c := &Client{
		id:       id,
		conn:     conn,
		outbound: make(chan []byte, clientQueueCapacity),
		done:     make(chan struct{}),
	}
	go c.writeLoop()
	return c
}

func (c *Client) EnqueueText(payload []byte) error {
	if len(payload) > maxFramePayload {
		return errors.New("websocket payload too large")
	}
	copyPayload := append([]byte(nil), payload...)
	select {
	case <-c.done:
		return ErrClientClosed
	case c.outbound <- copyPayload:
		return nil
	default:
		return ErrClientQueueFull
	}
}

func (c *Client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case payload := <-c.outbound:
			if err := c.WriteText(payload); err != nil {
				c.Close()
				return
			}
		}
	}
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *Client) SetDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }
