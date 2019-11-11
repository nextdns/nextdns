package endpoint

import (
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

func newTransport(e *Endpoint) transport {
	var addr string
	if e.Bootstrap != "" {
		addr = net.JoinHostPort(e.Bootstrap, "443")
	} else {
		addr = e.Hostname
	}
	t := &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: e.Hostname,
		},
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
