package endpoint

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"runtime"
)

type transport struct {
	http.RoundTripper
	hostname string
	path     string
	addr     string
}

func newTransport(e *DOHEndpoint) transport {
	var addr string
	var addrs []string
	if len(e.Bootstrap) != 0 {
		addr = net.JoinHostPort(e.Bootstrap[0], "443")
		for _, addr := range e.Bootstrap {
			addrs = append(addrs, net.JoinHostPort(addr, "443"))
		}
	} else {
		addr = e.Hostname
	}
	d := &parallelDialer{}
	t := &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: e.Hostname,
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addrs != nil {
				return d.DialParallel(ctx, network, addrs)
			}
			return d.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2: true,
	}
	runtime.SetFinalizer(t, func(t *http.Transport) {
		t.CloseIdleConnections()
	})
	return transport{
		RoundTripper: t,
		hostname:     e.Hostname,
		path:         e.Path,
		addr:         addr,
	}
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Host = t.addr
	req.Host = t.hostname
	if t.path != "" {
		req.URL.Path = t.path
	}
	return t.RoundTripper.RoundTrip(req)
}
