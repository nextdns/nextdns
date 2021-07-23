package endpoint

import (
	"crypto/x509"
	"embed"
	"sync"
)

var (
	//go:embed certs/*.pem
	rootCertificates embed.FS
	rootCAInit       sync.Once
	rootCAs          *x509.CertPool
)

func getRootCAs() *x509.CertPool {
	rootCAInit.Do(func() {
		certs, err := rootCertificates.ReadDir("certs")
		if err != nil || len(certs) == 0 {
			// No bundled certificates, let's use the
			// system certificates.
			return
		}

		rootCAs, _ = x509.SystemCertPool()
		if rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}
		for _, cert := range certs {
			pem, err := rootCertificates.ReadFile("certs/" + cert.Name())
			if err != nil {
				panic("cannot read cert: " + err.Error())
			}
			rootCAs.AppendCertsFromPEM(pem)
		}
	})
	return rootCAs
}
