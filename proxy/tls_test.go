package proxy

import (
	"crypto/x509"
	"os"
	"strings"
	"testing"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("generateSelfSignedCert() error: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("no certificates in TLS certificate")
	}

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate error: %v", err)
	}

	// Must include localhost as a DNS SAN.
	found := false
	for _, name := range x509Cert.DNSNames {
		if name == "localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DNSNames %v does not contain localhost", x509Cert.DNSNames)
	}

	// Must include system hostname if available.
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		found = false
		for _, name := range x509Cert.DNSNames {
			if name == hostname {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DNSNames %v does not contain system hostname %q", x509Cert.DNSNames, hostname)
		}

		// If FQDN, the short form should also be present.
		if short, _, ok := strings.Cut(hostname, "."); ok && short != hostname {
			found = false
			for _, name := range x509Cert.DNSNames {
				if name == short {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("DNSNames %v does not contain short hostname %q", x509Cert.DNSNames, short)
			}
		}
	}

	// Must include 127.0.0.1 and ::1 as IP SANs.
	has4, has6 := false, false
	for _, ip := range x509Cert.IPAddresses {
		if ip.IsLoopback() {
			if ip.To4() != nil {
				has4 = true
			} else {
				has6 = true
			}
		}
	}
	if !has4 {
		t.Error("IPAddresses does not contain 127.0.0.1")
	}
	if !has6 {
		t.Error("IPAddresses does not contain ::1")
	}

	// Verify cert metadata.
	if x509Cert.Subject.CommonName != "NextDNS Proxy" {
		t.Errorf("CommonName = %q, want %q", x509Cert.Subject.CommonName, "NextDNS Proxy")
	}
	if len(x509Cert.ExtKeyUsage) == 0 || x509Cert.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Error("missing ServerAuth extended key usage")
	}
}

func TestLoadTLSConfig_SelfSigned(t *testing.T) {
	cfg, err := TLSCertConfig{}.LoadTLSConfig()
	if err != nil {
		t.Fatalf("LoadTLSConfig() error: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
	if cfg.MinVersion != 0x0303 { // tls.VersionTLS12
		t.Errorf("MinVersion = %#x, want TLS 1.2 (%#x)", cfg.MinVersion, 0x0303)
	}
}

func TestLoadTLSConfig_InvalidFiles(t *testing.T) {
	_, err := TLSCertConfig{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}.LoadTLSConfig()
	if err == nil {
		t.Fatal("expected error for nonexistent cert files")
	}
}
