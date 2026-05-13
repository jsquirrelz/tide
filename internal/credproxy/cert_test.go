package credproxy

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMintSelfSignedCert_WritesAllThreeFiles verifies that MintSelfSignedCert
// creates cert.pem, ca.crt, and key.pem with non-zero size in the target dir.
func TestMintSelfSignedCert_WritesAllThreeFiles(t *testing.T) {
	dir := t.TempDir()
	if err := MintSelfSignedCert(dir, 24*time.Hour); err != nil {
		t.Fatalf("MintSelfSignedCert: unexpected error: %v", err)
	}
	for _, name := range []string{"cert.pem", "ca.crt", "key.pem"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("expected %s to have non-zero size", name)
		}
	}
}

// TestMintSelfSignedCert_RoundTripVia_LoadX509KeyPair verifies that the
// generated cert and key can be loaded as a valid tls.Certificate.
func TestMintSelfSignedCert_RoundTripVia_LoadX509KeyPair(t *testing.T) {
	dir := t.TempDir()
	if err := MintSelfSignedCert(dir, 24*time.Hour); err != nil {
		t.Fatalf("MintSelfSignedCert: %v", err)
	}
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	_, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair: %v", err)
	}
}

// TestMintSelfSignedCert_SANsIncludeLocalhostAnd127001 parses the generated
// cert and asserts that DNS:localhost and IP:127.0.0.1 are present in the SANs.
func TestMintSelfSignedCert_SANsIncludeLocalhostAnd127001(t *testing.T) {
	dir := t.TempDir()
	if err := MintSelfSignedCert(dir, 24*time.Hour); err != nil {
		t.Fatalf("MintSelfSignedCert: %v", err)
	}
	certBytes, err := os.ReadFile(filepath.Join(dir, "cert.pem"))
	if err != nil {
		t.Fatalf("ReadFile cert.pem: %v", err)
	}
	block, _ := pem.Decode(certBytes)
	if block == nil {
		t.Fatal("failed to decode PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate: %v", err)
	}

	// Check DNS SANs include localhost.
	foundLocalhost := false
	for _, dns := range cert.DNSNames {
		if dns == "localhost" {
			foundLocalhost = true
			break
		}
	}
	if !foundLocalhost {
		t.Errorf("expected DNS SAN 'localhost', got %v", cert.DNSNames)
	}

	// Check IP SANs include 127.0.0.1.
	found127 := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			found127 = true
			break
		}
	}
	if !found127 {
		t.Errorf("expected IP SAN 127.0.0.1, got %v", cert.IPAddresses)
	}
}

// TestMintSelfSignedCert_IsIdempotent verifies that calling MintSelfSignedCert
// twice into the same directory succeeds and the files exist after both calls.
func TestMintSelfSignedCert_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := MintSelfSignedCert(dir, 24*time.Hour); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := MintSelfSignedCert(dir, 24*time.Hour); err != nil {
		t.Fatalf("second call: %v", err)
	}
	for _, name := range []string{"cert.pem", "ca.crt", "key.pem"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to exist after second call: %v", name, err)
		}
	}
}
