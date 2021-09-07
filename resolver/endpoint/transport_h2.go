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

func newTransportH2(e *DOHEndpoint, addrs []string) http.RoundTripper {
	d := &parallelDialer{}
	d.FallbackDelay = -1 // disable happy eyeball, we do our own
	var t http.RoundTripper = &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName:         e.Hostname,
			ClientSessionCache: tls.NewLRUClientSessionCache(0),
		},
		DialContext: func(ctx context.Context, network, _ string) (c net.Conn, err error) {
			c, err = d.DialParallel(ctx, network, addrs)
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
	if e.onConnect != nil {
		t = roundTripperConnectTracer{
			RoundTripper: t,
			OnConnect:    e.onConnect,
		}
	}
	return t
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Host = t.addr
	req.Host = t.hostname
	if t.path != "" {
		req.URL.Path = t.path
	}
	return t.RoundTripper.RoundTrip(req)
}

func endpointAddrs(e *DOHEndpoint) (addrs []string) {
	if len(e.Bootstrap) != 0 {
		for _, addr := range e.Bootstrap {
			addrs = append(addrs, net.JoinHostPort(addr, "443"))
		}
	} else {
		addrs = []string{net.JoinHostPort(e.Hostname, "443")}
	}
	return addrs
}
