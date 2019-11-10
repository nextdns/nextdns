package endpoint

import (
	"crypto/tls"
	"net"
	"net/http"
)

type Transport struct {
	http.RoundTripper
	hostname string
	path     string
	addr     string
}

func NewTransport(e Endpoint) Transport {
	var addr string
	if e.Bootstrap != "" {
		addr = net.JoinHostPort(e.Bootstrap, "443")
	} else {
		addr = e.Hostname
	}
	return Transport{
		RoundTripper: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName: e.Hostname,
			},
		},
		hostname: e.Hostname,
		path:     e.Path,
		addr:     addr,
	}
}

func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Host = t.addr
	req.Host = t.hostname
	if t.path != "" {
		req.URL.Path = t.path
	}
	return t.RoundTripper.RoundTrip(req)
}
