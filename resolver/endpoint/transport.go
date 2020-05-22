package endpoint

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"runtime"
	"time"
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
	d.FallbackDelay = -1 // disable happy eyeball, we do our own
	t := &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: e.Hostname,
		},
		DialContext: func(ctx context.Context, network, addr string) (c net.Conn, err error) {
			if addrs != nil {
				c, err = d.DialParallel(ctx, network, addrs)
			} else {
				c, err = d.DialContext(ctx, network, addr)
			}
			if c != nil {
				// Try to workaround the bug describe in this issue:
				// https://github.com/golang/go/issues/23559
				//
				// All write operations are surrounded with a 5s deadline that
				// will close the h2 connection if reached. This is not a proper
				// fix but an attempt to mitigate the issue waiting for an
				// upstream fix.
				//
				// See #196 for more info.
				c = deadlineConn{
					Conn:    c,
					timeout: 5 * time.Second,
				}
			}
			return c, err
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
