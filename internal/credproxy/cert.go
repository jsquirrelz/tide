package credproxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// MintSelfSignedCert generates an ECDSA P-256 key + self-signed certificate
// and writes cert.pem, ca.crt (identical content), and key.pem into dir.
//
// Called by cmd/credproxy/main.go at sidecar startup (D-C2). Re-minting on
// every Pod start is intentional — Pod lifetime is bounded by Job completion
// and a fresh cert is cheaper than rotation logic (Pitfall 11 note).
//
// The cert carries:
//   - Subject CN: tide-credproxy
//   - DNS SANs: localhost
//   - IP SANs: 127.0.0.1, ::1
//   - KeyUsage: DigitalSignature + CertSign
//   - ExtKeyUsage: ServerAuth
//   - IsCA: true (acts as its own CA so the subagent can trust via ca.crt)
//   - NotBefore: now minus 1 minute (clock-skew tolerance)
//   - NotAfter:  now plus validity
func MintSelfSignedCert(dir string, validity time.Duration) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "tide-credproxy"},
		NotBefore:    time.Now().Add(-1 * time.Minute),
		NotAfter:     time.Now().Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:         true,
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}

	if err := writePEM(filepath.Join(dir, "cert.pem"), "CERTIFICATE", derBytes); err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", derBytes); err != nil {
		return err
	}
	return writePEM(filepath.Join(dir, "key.pem"), "EC PRIVATE KEY", keyDER)
}

// writePEM encodes der as a PEM block of blockType and writes it to path,
// overwriting any existing file.
func writePEM(path, blockType string, der []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}
