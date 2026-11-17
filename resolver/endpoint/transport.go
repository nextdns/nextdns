package endpoint

import "net/http"

func newTransport(e *DOHEndpoint) transport {
	addrs := endpointAddrs(e)
	return transport{
		RoundTripper: newTransportH2(e, addrs, nil),
		proxied:      newTransportH2(e, nil, http.ProxyFromEnvironment),
		proxy:        http.ProxyFromEnvironment,
		hostname:     e.Hostname,
		path:         e.Path,
		addr:         addrs[0],
	}
}
