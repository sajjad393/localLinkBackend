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
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
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
	var nets []*net.Interface
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
		// Bundle 300: ListenMulticastUDP switches multicast loopback off.
		// On Windows that hides the questions this PC itself asks (its own
		// browser); on Linux it hides our answer from a local asker. Turn
		// it back on; our own answers coming back are ignored (responses).
		if err := setLoop300(c); err != nil {
			log.Printf("MDNS_299 loop %s: %v (this PC's own browser may not find the name)", ifi.Name, err)
		}
		conns = append(conns, c)
		nets = append(nets, &ifi)
	}
	if len(conns) == 0 {
		return nil, errors.New("no network accepted multicast")
	}
	var mu sync.Mutex // one reply at a time; replies are tiny
	seen := newAskLog300()
	for k, c := range conns {
		go func(c *net.UDPConn, ifi *net.Interface) {
			buf := make([]byte, 1500)
			for {
				n, src, err := c.ReadFromUDP(buf)
				if err != nil {
					return
				}
				addrs, _ := ifi.Addrs()
				out, to, ip := reply300(buf[:n], src, name, addrs)
				if out == nil {
					continue
				}
				if line := seen.line(src, name, ip, to == group, time.Now()); line != "" {
					log.Print(line)
				}
				mu.Lock()
				_, _ = c.WriteToUDP(out, to)
				mu.Unlock()
			}
		}(c, nets[k])
	}
	log.Printf("MDNS_299 answering %s on %d network(s)", name, len(conns))
	return func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}, nil
}

// reply300 decides the answer for one packet that arrived on a network with
// addrs: the bytes, where to send them and the IP we announced. nil = not
// for us (other name, a response, or an asker outside this network).
//   - legacy one-shot (Android, src port != 5353): unicast to its own port.
//   - QU bit from another device: unicast to it (RFC 6762 5.4).
//   - everything else, and any 5353 question from THIS PC: multicast.
//     Windows' own DNS client shares port 5353 with us, so a unicast
//     answer to this PC's 5353 could land on our own socket instead.
func reply300(msg []byte, src *net.UDPAddr, name string, addrs []net.Addr) ([]byte, *net.UDPAddr, net.IP) {
	ip := localIPFor(src.IP, addrs)
	if ip == nil {
		return nil, nil, nil // not from this network
	}
	legacy := src.Port != 5353
	out := buildAnswer(msg, name, ip, legacy)
	if out == nil {
		return nil, nil, nil
	}
	if legacy || (asksDirect(msg) && !isOwnIP300(src.IP, addrs)) {
		return out, src, ip
	}
	return out, group, ip
}

func isOwnIP300(ip net.IP, addrs []net.Addr) bool {
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// askLog300 prints "MDNS_299 asked by ..." once per asker per 10 minutes,
// so the server window shows whether a phone's question arrived at all.
type askLog300 struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newAskLog300() *askLog300 { return &askLog300{last: map[string]time.Time{}} }

func (l *askLog300) line(src *net.UDPAddr, name string, ip net.IP, multicast bool, now time.Time) string {
	key := src.IP.String()
	l.mu.Lock()
	defer l.mu.Unlock()
	if t, ok := l.last[key]; ok && now.Sub(t) < 10*time.Minute {
		return ""
	}
	if len(l.last) > 256 {
		l.last = map[string]time.Time{}
	}
	l.last[key] = now
	kind, how := "mdns", "unicast"
	if src.Port != 5353 {
		kind = "legacy"
	}
	if multicast {
		how = "multicast"
	}
	return fmt.Sprintf("MDNS_299 asked by %s for %s (%s) -> answered %s (%s)", key, name, kind, ip, how)
}

// asksDirect reports the QU bit ("answer me directly") on the first question.
func asksDirect(msg []byte) bool {
	_, next, err := readName(msg, 12)
	if err != nil || next+4 > len(msg) {
		return false
	}
	return binary.BigEndian.Uint16(msg[next+2:])&0x8000 != 0
}
