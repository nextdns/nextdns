package resolver

import (
	"context"
	"net"
)

// DNS is a DNS53 implementation of the Resolver interface.
type DNS struct {
	Addr *net.UDPAddr
}

// Resolve implements the Resolver interface.
func (r DNS) Resolve(ctx context.Context, q Query, buf []byte) (int, error) {
	// TODO: support context cancelation
	c, err := net.DialUDP("udp", nil, r.Addr)
	if err != nil {
		return -1, err
	}
	defer c.Close()
	_, err = c.Write(q.Payload)
	if err != nil {
		return -1, err
	}
	return c.Read(buf)
}
