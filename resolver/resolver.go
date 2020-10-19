package resolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/nextdns/nextdns/resolver/endpoint"
	"github.com/nextdns/nextdns/resolver/query"
)

// Resolver is an interface to a type that send q to a resolver using a specific
// transport.
type Resolver interface {
	// Resolve send q and write the response into buf. If buf too small,
	// response is truncated. It is fine to reuse the same []byte for q.Payload
	// and buf.
	//
	// When caching is enabled, a cached response is returned if a valid entry
	// is found in the cache for q. In case of err with cache enabled, an
	// expired fallback entry may be stored in buf. In such case, n is > 0.
	Resolve(ctx context.Context, q query.Query, buf []byte) (n int, i ResolveInfo, err error)
}

type Cacher interface {
	Add(key, value interface{})
	Get(key interface{}) (value interface{}, ok bool)
}

type CacheStats struct {
	Hit  uint32 `json:"hit"`
	Miss uint32 `json:"miss"`
}

type DNS struct {
	DOH        DOH
	DNS53      DNS53
	Manager    *endpoint.Manager
	cacheStats CacheStats
}

type ResolveInfo struct {
	Transport string
	FromCache bool
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
func (r *DNS) Resolve(ctx context.Context, q query.Query, buf []byte) (n int, i ResolveInfo, err error) {
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
	if err == nil {
		if i.FromCache {
			atomic.AddUint32(&r.cacheStats.Hit, 1)
		} else {
			atomic.AddUint32(&r.cacheStats.Miss, 1)
		}
	}
	return n, i, err
}

func (r *DNS) CacheStats() CacheStats {
	return r.cacheStats
}
