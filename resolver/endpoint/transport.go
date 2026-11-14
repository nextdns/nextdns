package endpoint

import "net"

func newTransport(e *DOHEndpoint) transport {
	addrs := endpointAddrs(e)
	addr := addrs[0]
	if httpProxyFromEnv() {
		// Prefer the hostname for CONNECT when a proxy is configured so
		// corporate proxies that filter by name keep working.
		addr = net.JoinHostPort(e.Hostname, "443")
	}
	return transport{
		RoundTripper: newTransportH2(e, addrs),
		hostname:     e.Hostname,
		path:         e.Path,
		addr:         addr,
	}
}
