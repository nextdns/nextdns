package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nextdns/nextdns/resolver/endpoint"
)

// Resolver is an interface to a type that send q to a resolver using a specific
// transport.
type Resolver interface {
	// Resolve send q and write the response into buf. If buf too small,
	// response is truncated. It is fine to reuse the same []byte for
	// q.Payload and buf.
	Resolve(ctx context.Context, q Query, buf []byte) (n int, i ResolveInfo, err error)
}

type DNS struct {
	DOH     DOH
	DNS53   DNS53
	Manager *endpoint.Manager
}

type ResolveInfo struct {
	Transport string
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
	return &DNS{
		Manager: &endpoint.Manager{
			Providers: []endpoint.Provider{endpoint.StaticProvider(endpoints)},
		},
	}, nil
}

// Resolve implements Resolver interface.
func (r *DNS) Resolve(ctx context.Context, q Query, buf []byte) (n int, i ResolveInfo, err error) {
	err = r.Manager.Do(ctx, func(e endpoint.Endpoint) error {
		var err2 error
		switch e := e.(type) {
		case *endpoint.DOHEndpoint:
			if n, i, err2 = r.DOH.resolve(ctx, q, buf, e); err2 != nil {
				return fmt.Errorf("doh resolve: %v", err2)
			}
		case *endpoint.DNSEndpoint:
			if n, i, err2 = r.DNS53.resolve(ctx, q, buf, e.Addr); err2 != nil {
				return fmt.Errorf("dns resolve: %v", err2)
			}
		default:
			return fmt.Errorf("dns resolve: unsupported type: %T", e)
		}
		return nil
	})
	return n, i, err
}
