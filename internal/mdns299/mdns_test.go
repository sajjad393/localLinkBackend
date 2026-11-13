package mdns299

import (
	"encoding/binary"
	"net"
	"testing"
)

func query(id uint16, name string, qtype uint16, qu bool) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b, id)
	binary.BigEndian.PutUint16(b[4:], 1)
	b = append(b, encodeName(name)...)
	b = binary.BigEndian.AppendUint16(b, qtype)
	c := uint16(classIN)
	if qu {
		c |= 0x8000
	}
	return binary.BigEndian.AppendUint16(b, c)
}

var ip = net.IPv4(192, 168, 18, 27)

func TestAnswersOwnNameAnyCase(t *testing.T) {
	out := buildAnswer(query(0, "LinkUp.local", typeA, false), "linkup.local", ip, false)
	if out == nil {
		t.Fatal("no answer")
	}
	if binary.BigEndian.Uint16(out[2:]) != 0x8400 || binary.BigEndian.Uint16(out[6:]) != 1 {
		t.Fatalf("header % x", out[:12])
	}
	name, off, err := readName(out, 12)
	if err != nil || name != "linkup.local" {
		t.Fatalf("name %q %v", name, err)
	}
	if binary.BigEndian.Uint16(out[off:]) != typeA || binary.BigEndian.Uint16(out[off+2:]) != classIN|0x8000 {
		t.Fatal("type/class")
	}
	if got := net.IP(out[off+10 : off+14]); !got.Equal(ip) {
		t.Fatalf("ip %v", got)
	}
}

func TestIgnoresOtherNamesAndResponses(t *testing.T) {
	if buildAnswer(query(0, "printer.local", typeA, false), "linkup.local", ip, false) != nil {
		t.Fatal("answered another name")
	}
	resp := query(0, "linkup.local", typeA, false)
	resp[2] = 0x84
	if buildAnswer(resp, "linkup.local", ip, false) != nil {
		t.Fatal("answered a response")
	}
	if buildAnswer([]byte{1, 2, 3}, "linkup.local", ip, false) != nil {
		t.Fatal("answered garbage")
	}
	bad := query(0, "linkup.local", typeA, false)
	bad[12] = 0xC0 // pointer loop to itself
	bad[13] = 12
	if buildAnswer(bad, "linkup.local", ip, false) != nil {
		t.Fatal("answered a pointer loop")
	}
}

// Android's resolver sends a one-shot query from a random port: the reply
// must echo the id and the question, TTL 10, no cache-flush bit.
func TestLegacyOneShotEchoesQuestion(t *testing.T) {
	out := buildAnswer(query(0xBEEF, "linkup.local", typeA, false), "linkup.local", ip, true)
	if binary.BigEndian.Uint16(out) != 0xBEEF || binary.BigEndian.Uint16(out[4:]) != 1 {
		t.Fatalf("header % x", out[:12])
	}
	_, off, _ := readName(out, 12)
	off += 4 // the echoed question
	_, off, _ = readName(out, off)
	if binary.BigEndian.Uint16(out[off+2:]) != classIN || binary.BigEndian.Uint32(out[off+4:]) != 10 {
		t.Fatal("legacy class/ttl")
	}
}

// AAAA: say "only IPv4" at once (NSEC) and give the A record as extra.
func TestAAAAGetsNSECAndExtraA(t *testing.T) {
	out := buildAnswer(query(0, "linkup.local", typeAAAA, false), "linkup.local", ip, false)
	if binary.BigEndian.Uint16(out[6:]) != 1 || binary.BigEndian.Uint16(out[10:]) != 1 {
		t.Fatalf("counts % x", out[:12])
	}
	_, off, _ := readName(out, 12)
	if binary.BigEndian.Uint16(out[off:]) != typeNSEC {
		t.Fatal("want NSEC first")
	}
}

func TestAsksDirectAndLocalIP(t *testing.T) {
	if !asksDirect(query(0, "linkup.local", typeA, true)) || asksDirect(query(0, "linkup.local", typeA, false)) {
		t.Fatal("QU bit")
	}
	_, wifi, _ := net.ParseCIDR("192.168.18.27/24")
	wifi.IP = ip
	_, vpn, _ := net.ParseCIDR("10.8.0.2/24")
	vpn.IP = net.IPv4(10, 8, 0, 2)
	addrs := []net.Addr{vpn, wifi}
	if got := localIPFor(net.IPv4(192, 168, 18, 33), addrs); !got.Equal(ip) {
		t.Fatalf("got %v", got)
	}
	if localIPFor(net.IPv4(172, 16, 0, 5), addrs) != nil {
		t.Fatal("answered a stranger network")
	}
}
