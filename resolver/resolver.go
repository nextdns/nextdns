package resolver

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// Resolver is an interface to a type that send q to a resolver using a specific
// transport.
type Resolver interface {
	// Resolve send q and write the response into buf. If buf too small,
	// response is truncated. It is fine to reuse the same []byte for q.Payload
	// and buf.
	Resolve(ctx context.Context, q Query, buf []byte) (n int, err error)
}

// New instancies a DNS53 or DoH resolver for addr.
func New(addr string) (Resolver, error) {
	if strings.HasPrefix(addr, "https://") {
		return DOH{URL: addr}, nil
	} else if ip := net.ParseIP(addr); ip != nil {
		return DNS{Addr: &net.UDPAddr{IP: ip, Port: 53}}, nil
	}
	return nil, fmt.Errorf("%s: unsupported resolver address", addr)
}
