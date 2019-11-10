package resolver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/nextdns/nextdns/resolver/endpoint"
)

// Resolver is an interface to a type that send q to a resolver using a specific
// transport.
type Resolver interface {
	// Resolve send q and write the response into buf. If buf too small,
	// response is truncated. It is fine to reuse the same []byte for q.Payload
	// and buf.
	Resolve(ctx context.Context, q Query, buf []byte) (n int, err error)
}

// New instances a DNS53 or DoH resolver for endpoint.
//
// Supported format for servers are:
//
//   * DoH:   https://doh.server.com/path
//   * DoH:   https://doh.server.com/path#1.2.3.4 // with bootstrap
//   * DoH:   https://doh.server.com/path,https://doh2.server.com/path
//   * DNS53: 1.2.3.4
//   * DNS53: 1.2.3.4,1.2.3.5
//
func New(servers string) (Resolver, error) {
	var endpoints []endpoint.Endpoint
	for _, addr := range strings.Split(servers, ",") {
		e, err := endpoint.New(strings.TrimSpace(addr))
		if err != nil {
			return nil, fmt.Errorf("%s: unsupported resolver address: %v", addr, err)
		}
		endpoints = append(endpoints, e)
	}
	if len(endpoints) == 0 {
		return nil, errors.New("empty server list")
	}
	proto := endpoints[0].Protocol
	for _, e := range endpoints {
		if e.Protocol != proto {
			return nil, errors.New("cannot mix DoH and DNS servers")
		}
	}
	switch proto {
	case endpoint.ProtocolDOH:
		r := DOH{URL: fmt.Sprintf("https://%s%s", endpoints[0].Hostname, endpoints[0].Path)}
		if len(endpoints) > 1 {
			r.Transport = &endpoint.Manager{
				Providers: []endpoint.Provider{endpoint.StaticProvider(endpoints)},
			}
		}
		return r, nil
	case endpoint.ProtocolDNS:
		return DNS{Endpoint: &endpoint.Manager{
			Providers: []endpoint.Provider{endpoint.StaticProvider(endpoints)},
			OnChange:  func(e endpoint.Endpoint) { log.Print("change", e) },
			OnError:   func(e endpoint.Endpoint, err error) { log.Print("err", e, err) },
		}}, nil
	default:
		panic("unsupported protocol")
	}
}
