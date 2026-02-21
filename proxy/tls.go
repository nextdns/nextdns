package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

// TLSCertConfig describes how to obtain a TLS certificate for the DoT/DoH
// listeners.
type TLSCertConfig struct {
	// CertFile is the path to a PEM-encoded certificate file.
	CertFile string

	// KeyFile is the path to a PEM-encoded private key file.
	KeyFile string
}

// LoadTLSConfig returns a *tls.Config based on the configuration. If CertFile
// and KeyFile are set, the certificate is loaded from disk. Otherwise a
// self-signed certificate is generated.
func (c TLSCertConfig) LoadTLSConfig() (*tls.Config, error) {
	var cert tls.Certificate
	var err error

	if c.CertFile != "" && c.KeyFile != "" {
		cert, err = tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load tls cert: %w", err)
		}
	} else {
		cert, err = generateSelfSignedCert()
		if err != nil {
			return nil, fmt.Errorf("generate self-signed cert: %w", err)
		}
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
	}, nil
}

// generateSelfSignedCert creates a self-signed EC P-256 certificate valid for
// 1 year with SANs for localhost, the system hostname (FQDN and short form),
// 127.0.0.1, ::1, and any non-loopback LAN IPs found on the machine.
func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "NextDNS Proxy",
			Organization: []string{"NextDNS"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	// Add system hostname (FQDN and short form) as SANs so clients can
	// connect using the machine's hostname.
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		template.DNSNames = append(template.DNSNames, hostname)
		if short, _, ok := strings.Cut(hostname, "."); ok && short != hostname {
			template.DNSNames = append(template.DNSNames, short)
		}
	}

	// Add LAN IPs as SANs so clients on the local network can connect
	// without certificate errors (if they trust this cert).
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
					template.IPAddresses = append(template.IPAddresses, ipNet.IP)
				}
			}
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}
