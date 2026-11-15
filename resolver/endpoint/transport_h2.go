package endpoint

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"time"
)

type transport struct {
	http.RoundTripper
	hostname string
	path     string
	addr     string

	// proxy selects the proxy for each DoH request. When it returns one, the
	// request goes through proxied to the endpoint hostname instead of the
	// bootstrap addresses.
	proxy   func(*http.Request) (*url.URL, error)
	proxied http.RoundTripper
}

// newTransportH2 dials addrs directly or, when proxy is set, dials the
// address chosen by http.Transport (the proxy) and lets it send CONNECT.
func newTransportH2(e *DOHEndpoint, addrs []string, proxy func(*http.Request) (*url.URL, error)) http.RoundTripper {
	d := &parallelDialer{}
	d.FallbackDelay = -1 // disable happy eyeball, we do our own
	var t http.RoundTripper = &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName:         e.Hostname,
			RootCAs:            getRootCAs(),
			ClientSessionCache: tls.NewLRUClientSessionCache(0),
			MinVersion:         tls.VersionTLS13,
		},
		Proxy: proxy,
		DialContext: func(ctx context.Context, network, addr string) (c net.Conn, err error) {
			if proxy != nil {
				c, err = d.DialContext(ctx, network, addr)
			} else {
				c, err = d.DialParallel(ctx, network, addrs)
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
		// A write can succeed into the socket buffer even when the connection
		// is blackholed. Check peer liveness independently of request deadlines.
		HTTP2: &http.HTTP2Config{
			SendPingTimeout: 10 * time.Second,
			PingTimeout:     5 * time.Second,
		},
		// Bound unused connections, including those opened by endpoint probes.
		// Live HTTP/2 connections retain the transport, preventing finalization.
		IdleConnTimeout: 90 * time.Second,
	}
	runtime.SetFinalizer(t, func(t *http.Transport) {
		t.CloseIdleConnections()
	})
	t = roundTripperConnectTracer{
		RoundTripper: t,
		OnConnect: func(ci *ConnectInfo) {
			if onConnect := e.getOnConnect(); onConnect != nil {
				onConnect(ci)
			}
		},
	}
	return t
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Host = t.hostname
	req.Host = t.hostname
	if t.path != "" {
		req.URL.Path = t.path
	}
	if t.proxy != nil {
		proxyURL, err := t.proxy(req)
		if err != nil {
			return nil, err
		}
		if proxyURL != nil {
			return t.proxied.RoundTrip(req)
		}
	}
	req.URL.Host = t.addr
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
