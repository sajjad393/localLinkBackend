// Package mdns299 answers "linkup.local" on the Wi-Fi (Bundle 299 N).
//
// Phones with Android 12+ and Windows 10+ PCs ask the whole Wi-Fi
// "who is linkup.local?" (multicast DNS, RFC 6762). This tiny responder
// answers with the server's own IP on the network the question came from,
// so typing linkup.local/download opens the server. Nothing else is
// announced. Standard library only. If port 5353 cannot be opened the
// server keeps working on its IP and logs MDNS_299 off.
package mdns299

import (
	"encoding/binary"
	"errors"
	"log"
	"net"
	"strings"
	"sync"
)

const (
	typeA    = 1
	typeAAAA = 28
	typeNSEC = 47
	typeANY  = 255
	classIN  = 1
)

var group = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// question is one parsed question of a query.
type question struct {
	name  string // lower case, no trailing dot
	qtype uint16
	raw   []byte // the question bytes as sent (for legacy echo)
}

// readName reads a (possibly compressed) DNS name at off.
func readName(msg []byte, off int) (string, int, error) {
	var labels []string
	end := -1
	for jumps := 0; ; {
		if off >= len(msg) {
			return "", 0, errors.New("short")
		}
		l := int(msg[off])
		switch {
		case l == 0:
			if end < 0 {
				end = off + 1
			}
			return strings.ToLower(strings.Join(labels, ".")), end, nil
		case l&0xC0 == 0xC0:
			if off+1 >= len(msg) || jumps > 10 {
				return "", 0, errors.New("bad pointer")
			}
			if end < 0 {
				end = off + 2
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)
			jumps++
		case l > 63:
			return "", 0, errors.New("bad label")
		default:
			if off+1+l > len(msg) {
				return "", 0, errors.New("short label")
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
}

// parseQuery returns the questions of a query (nil for answers/garbage).
func parseQuery(msg []byte) (id uint16, qs []question) {
	if len(msg) < 12 || msg[2]&0x80 != 0 { // responses are ignored
		return 0, nil
	}
	id = binary.BigEndian.Uint16(msg)
	n := int(binary.BigEndian.Uint16(msg[4:]))
	off := 12
	for i := 0; i < n && i < 32; i++ {
		start := off
		name, next, err := readName(msg, off)
		if err != nil || next+4 > len(msg) {
			return id, qs
		}
		qt := binary.BigEndian.Uint16(msg[next:])
		qc := binary.BigEndian.Uint16(msg[next+2:]) & 0x7FFF
		off = next + 4
		if qc == classIN || qc == typeANY {
			qs = append(qs, question{name: name, qtype: qt, raw: append([]byte(nil), msg[start:off]...)})
		}
	}
	return id, qs
}

func encodeName(name string) []byte {
	var b []byte
	for _, l := range strings.Split(name, ".") {
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0)
}

func rr(name string, rtype, class uint16, ttl uint32, rdata []byte) []byte {
	b := encodeName(name)
	b = binary.BigEndian.AppendUint16(b, rtype)
	b = binary.BigEndian.AppendUint16(b, class)
	b = binary.BigEndian.AppendUint32(b, ttl)
	b = binary.BigEndian.AppendUint16(b, uint16(len(rdata)))
	return append(b, rdata...)
}

// buildAnswer makes the reply for a query asking about name, or nil when
// the query is not for us. legacy = one-shot query from a port other than
// 5353 (Android's resolver): echo id + question, short TTL, no cache-flush
// bit (RFC 6762 6.7). An AAAA question gets "A only" (NSEC) so the phone
// does not wait for an IPv6 answer.
func buildAnswer(msg []byte, name string, ip net.IP, legacy bool) []byte {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil
	}
	id, qs := parseQuery(msg)
	var echo [][]byte
	wantA, wantAAAA := false, false
	for _, q := range qs {
		if q.name != name {
			continue
		}
		switch q.qtype {
		case typeA, typeANY:
			wantA = true
		case typeAAAA:
			wantAAAA = true
		default:
			continue
		}
		echo = append(echo, q.raw)
	}
	if !wantA && !wantAAAA {
		return nil
	}
	class, ttl := uint16(classIN|0x8000), uint32(120)
	if legacy {
		class, ttl = classIN, 10
	} else {
		id = 0
		echo = nil
	}
	nsec := rr(name, typeNSEC, class, ttl, append(encodeName(name), 0, 1, 0x40)) // window 0: A only
	a := rr(name, typeA, class, ttl, ip4)
	var answers, additional [][]byte
	if wantA {
		answers = append(answers, a)
		if wantAAAA {
			answers = append(answers, nsec)
		} else {
			additional = append(additional, nsec)
		}
	} else {
		answers = append(answers, nsec)
		additional = append(additional, a)
	}
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[0:], id)
	binary.BigEndian.PutUint16(b[2:], 0x8400) // response, authoritative
	binary.BigEndian.PutUint16(b[4:], uint16(len(echo)))
	binary.BigEndian.PutUint16(b[6:], uint16(len(answers)))
	binary.BigEndian.PutUint16(b[10:], uint16(len(additional)))
	for _, q := range echo {
		b = append(b, q...)
	}
	for _, r := range answers {
		b = append(b, r...)
	}
	for _, r := range additional {
		b = append(b, r...)
	}
	return b
}

// localIPFor picks this PC's IPv4 on the same network as the asker.
func localIPFor(src net.IP, addrs []net.Addr) net.IP {
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.Contains(src) {
			return n.IP.To4()
		}
	}
	return nil
}

// Start answers name (for example "linkup.local") on every Wi-Fi/LAN
// network. It returns a stop function. Errors only mean "no short name";
// the server itself is not affected.
func Start(name string) (func(), error) {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var conns []*net.UDPConn
	for i := range ifaces {
		ifi := ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		c, err := net.ListenMulticastUDP("udp4", &ifi, group)
		if err != nil {
			log.Printf("MDNS_299 skip %s: %v", ifi.Name, err)
			continue
		}
		conns = append(conns, c)
	}
	if len(conns) == 0 {
		return nil, errors.New("no network accepted multicast")
	}
	var mu sync.Mutex // one reply at a time; replies are tiny
	for _, c := range conns {
		go func(c *net.UDPConn) {
			buf := make([]byte, 1500)
			for {
				n, src, err := c.ReadFromUDP(buf)
				if err != nil {
					return
				}
				addrs, _ := net.InterfaceAddrs()
				ip := localIPFor(src.IP, addrs)
				if ip == nil {
					continue // not from a network we are on
				}
				legacy := src.Port != 5353
				out := buildAnswer(buf[:n], name, ip, legacy)
				if out == nil {
					continue
				}
				unicast := legacy || asksDirect(buf[:n])
				to := group
				if unicast {
					to = src
				}
				mu.Lock()
				_, _ = c.WriteToUDP(out, to)
				mu.Unlock()
			}
		}(c)
	}
	log.Printf("MDNS_299 answering %s on %d network(s)", name, len(conns))
	return func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}, nil
}

// asksDirect reports the QU bit ("answer me directly") on the first question.
func asksDirect(msg []byte) bool {
	_, next, err := readName(msg, 12)
	if err != nil || next+4 > len(msg) {
		return false
	}
	return binary.BigEndian.Uint16(msg[next+2:])&0x8000 != 0
}
