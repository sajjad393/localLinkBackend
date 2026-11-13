package mdns299

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func wifi300() []net.Addr {
	_, n, _ := net.ParseCIDR("192.168.18.27/24")
	n.IP = net.IPv4(192, 168, 18, 27).To4()
	return []net.Addr{n}
}

// Bundle 300: who gets the answer.
func TestReply300Routing(t *testing.T) {
	addrs := wifi300()
	self := &net.UDPAddr{IP: net.IPv4(192, 168, 18, 27), Port: 5353}
	phone := &net.UDPAddr{IP: net.IPv4(192, 168, 18, 40), Port: 5353}
	oneShot := &net.UDPAddr{IP: net.IPv4(192, 168, 18, 40), Port: 41234}
	stranger := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 5), Port: 5353}

	// This PC asking (Windows DNS client, QU bit): multicast, never unicast to our shared 5353.
	if out, to, got := reply300(query(0, "linkup.local", typeA, true), self, "linkup.local", addrs); out == nil || to != group || !got.Equal(net.IPv4(192, 168, 18, 27)) {
		t.Fatalf("self QU: out=%v to=%v ip=%v", out != nil, to, got)
	}
	if out, to, _ := reply300(query(0, "linkup.local", typeA, false), self, "linkup.local", addrs); out == nil || to != group {
		t.Fatalf("self QM: to=%v", to)
	}
	// Another device with QU: unicast to it (unchanged 299 behaviour).
	if out, to, _ := reply300(query(0, "linkup.local", typeA, true), phone, "linkup.local", addrs); out == nil || to != phone {
		t.Fatalf("phone QU: to=%v", to)
	}
	if out, to, _ := reply300(query(0, "linkup.local", typeA, false), phone, "linkup.local", addrs); out == nil || to != group {
		t.Fatalf("phone QM: to=%v", to)
	}
	// Android one-shot: unicast to its own port, id echoed (unchanged).
	out, to, _ := reply300(query(0x1234, "linkup.local", typeA, false), oneShot, "linkup.local", addrs)
	if out == nil || to != oneShot || binary.BigEndian.Uint16(out) != 0x1234 {
		t.Fatalf("one-shot: to=%v", to)
	}
	// Not for us.
	if out, _, _ := reply300(query(0, "linkup.local", typeA, false), stranger, "linkup.local", addrs); out != nil {
		t.Fatal("answered a stranger network")
	}
	if out, _, _ := reply300(query(0, "printer.local", typeA, false), phone, "linkup.local", addrs); out != nil {
		t.Fatal("answered another name")
	}
}

func TestAskLog300OncePerAsker(t *testing.T) {
	l := newAskLog300()
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	phone := &net.UDPAddr{IP: net.IPv4(192, 168, 18, 40), Port: 41234}
	ip := net.IPv4(192, 168, 18, 27)
	want := "MDNS_299 asked by 192.168.18.40 for linkup.local (legacy) -> answered 192.168.18.27 (unicast)"
	if got := l.line(phone, "linkup.local", ip, false, now); got != want {
		t.Fatalf("got %q", got)
	}
	if got := l.line(phone, "linkup.local", ip, false, now.Add(9*time.Minute)); got != "" {
		t.Fatalf("repeated within 10 min: %q", got)
	}
	if got := l.line(phone, "linkup.local", ip, false, now.Add(11*time.Minute)); got == "" {
		t.Fatal("silent after 10 min")
	}
	pc := &net.UDPAddr{IP: ip, Port: 5353}
	if got := l.line(pc, "linkup.local", ip, true, now); !strings.Contains(got, "(mdns) -> answered 192.168.18.27 (multicast)") {
		t.Fatalf("got %q", got)
	}
}

// A real question from this same machine, asked the way Windows' DNS client
// does (from port 5353, loopback on), must get an answer. Before Bundle 300
// ListenMulticastUDP's loopback-off made this impossible.
func TestSameHostAskerGetsAnswer300(t *testing.T) {
	var ifi *net.Interface
	ifaces, _ := net.Interfaces()
	for i := range ifaces {
		f := ifaces[i]
		if f.Flags&net.FlagUp == 0 || f.Flags&net.FlagMulticast == 0 || f.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := f.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				ifi = &f
			}
		}
		if ifi != nil {
			break
		}
	}
	if ifi == nil {
		t.Skip("no multicast IPv4 network here (not faked)")
	}
	name := fmt.Sprintf("loop300-%d.local", os.Getpid())
	stop, err := Start(name)
	if err != nil {
		t.Skipf("cannot open 5353 here: %v", err)
	}
	defer stop()
	asker, err := net.ListenMulticastUDP("udp4", ifi, group)
	if err != nil {
		t.Skipf("second 5353 socket: %v", err)
	}
	defer asker.Close()
	if err := setLoop300(asker); err != nil {
		t.Fatal(err)
	}
	for _, qu := range []bool{true, false} {
		if _, err := asker.WriteToUDP(query(0, name, typeA, qu), group); err != nil {
			t.Skipf("send: %v", err)
		}
		_ = asker.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 1500)
		got := false
		for !got {
			n, _, err := asker.ReadFromUDP(buf)
			if err != nil {
				t.Fatalf("QU=%v: no answer from the responder on the same machine: %v", qu, err)
			}
			msg := buf[:n]
			if len(msg) < 12 || msg[2]&0x80 == 0 || binary.BigEndian.Uint16(msg[6:]) == 0 {
				continue // our own question looping back
			}
			if rn, off, err := readName(msg, 12); err == nil && rn == name && binary.BigEndian.Uint16(msg[off:]) == typeA {
				got = true
			}
		}
	}
}
