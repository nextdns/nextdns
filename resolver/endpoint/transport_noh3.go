//+build !http3

package endpoint

func newTransport(e *DOHEndpoint) transport {
	addrs := endpointAddrs(e)
	return transport{
		RoundTripper: newTransportH2(e, addrs),
		hostname:     e.Hostname,
		path:         e.Path,
		addr:         addrs[0],
	}
}
