package endpoint

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"

	quic "github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

func newTransportH3(e *DOHEndpoint, addrs []string) http.RoundTripper {
	if e.FastestIP != "" {
		fastestAddr := e.FastestIP
		if !strings.Contains(fastestAddr, ":") {
			fastestAddr = net.JoinHostPort(fastestAddr, "443")
		}
		filtered := make([]string, 0, len(addrs))
		for _, a := range addrs {
			if !strings.Contains(a, ":") {
				a = net.JoinHostPort(a, "443")
			}
			if a != fastestAddr {
				filtered = append(filtered, a)
			}
		}
		addrs = append([]string{fastestAddr}, filtered...)
	}
	return &http3.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: e.Hostname,
			RootCAs:    getRootCAs(),
			NextProtos: []string{"h3"}, // Ensure ALPN "h3" is offered for DoH3
		},
		Dial: func(ctx context.Context, addr string, tlsCfg *tls.Config, quicCfg *quic.Config) (quic.EarlyConnection, error) {
			var conn quic.EarlyConnection
			var err error
			if len(addrs) > 0 {
				conn, err = quic.DialAddrEarly(ctx, addrs[0], tlsCfg, quicCfg)
				return conn, err
			}
			conn, err = quic.DialAddrEarly(ctx, addr, tlsCfg, quicCfg)
			return conn, err
		},
	}
}
