// Package tlsauto (bundle 243, audit S3 part A): a self-signed certificate the
// server makes once, kept in data/tls/. Phones trust it by its SHA-256 hash,
// which the signed pairing proof (ll-pair-v2) vouches for - no public CA.
package tlsauto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	CertName = "cert.pem"
	KeyName  = "key.pem"
	validFor = 5 * 365 * 24 * time.Hour
)

// Ensure returns the certificate and key paths in dir and the hex SHA-256
// of the certificate (DER). An existing valid pair is reused, so the hash
// stays the same across restarts; a missing or broken pair is made again.
func Ensure(dir string) (certPath, keyPath, sha string, err error) {
	certPath, keyPath = filepath.Join(dir, CertName), filepath.Join(dir, KeyName)
	if sha, err = Fingerprint(certPath, keyPath); err == nil {
		return certPath, keyPath, sha, nil
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", "", "", err
	}
	if err = generate(certPath, keyPath, LANIPs()); err != nil {
		return "", "", "", err
	}
	sha, err = Fingerprint(certPath, keyPath)
	return certPath, keyPath, sha, err
}

// Fingerprint loads the pair and returns the leaf's SHA-256 (hex, lower
// case). It fails for a missing, unmatched or expired pair.
func Fingerprint(certPath, keyPath string) (string, error) {
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return "", err
	}
	if len(pair.Certificate) == 0 {
		return "", errors.New("no certificate")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "", err
	}
	if time.Now().After(leaf.NotAfter.Add(-24 * time.Hour)) {
		return "", errors.New("certificate expired")
	}
	sum := sha256.Sum256(pair.Certificate[0])
	return hex.EncodeToString(sum[:]), nil
}

// LANIPs lists the machine's IPv4 addresses (not loopback) for the SAN.
func LANIPs() []net.IP {
	out := []net.IP{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() {
			continue
		}
		if v4 := ipn.IP.To4(); v4 != nil {
			out = append(out, v4)
		}
	}
	return out
}

func generate(certPath, keyPath string, ips []net.IP) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "LinkUp"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           append([]net.IP{net.IPv4(127, 0, 0, 1)}, ips...),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}
