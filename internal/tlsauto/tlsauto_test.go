package tlsauto

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
)

func TestEnsureMakesOnceAndReuses(t *testing.T) {
	dir := t.TempDir()
	c1, k1, s1, err := Ensure(dir)
	if err != nil || len(s1) != 64 {
		t.Fatalf("first: %v %q", err, s1)
	}
	c2, k2, s2, err := Ensure(dir)
	if err != nil || s2 != s1 || c1 != c2 || k1 != k2 {
		t.Fatalf("must reuse the same certificate: %v %q %q", err, s1, s2)
	}
	pair, _ := tls.LoadX509KeyPair(c1, k1)
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	if leaf.Subject.CommonName != "LinkUp" || !leaf.IPAddresses[0].Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("bad cert: %v %v", leaf.Subject, leaf.IPAddresses)
	}
	for _, ip := range LANIPs() {
		found := false
		for _, x := range leaf.IPAddresses {
			found = found || x.Equal(ip)
		}
		if !found {
			t.Fatalf("SAN misses LAN IP %v", ip)
		}
	}
	if leaf.NotAfter.Sub(leaf.NotBefore).Hours() < 4*365*24 {
		t.Fatal("must be valid for years")
	}
}
