package resolver

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/nextdns/nextdns/resolver/endpoint"
)

type EndpointDoer interface {
	Do(ctx context.Context, action func(e *endpoint.Endpoint) error) error
}

// DNS is a DNS53 implementation of the Resolver interface.
type DNS struct {
	Endpoint EndpointDoer

	Dialer *net.Dialer
}

var defaultDialer = &net.Dialer{}

// Resolve implements the Resolver interface.
func (r DNS) Resolve(ctx context.Context, q Query, buf []byte) (n int, err error) {
	if doErr := r.Endpoint.Do(ctx, func(e *endpoint.Endpoint) error {
		if n, err = r.resolve(ctx, q, buf, e.Hostname); err != nil {
			err = fmt.Errorf("dns resolve: %v", err)
		}
		return err
	}); doErr != nil {
		return 0, doErr
	}
	return
}

func (r DNS) resolve(ctx context.Context, q Query, buf []byte, addr string) (int, error) {
	d := r.Dialer
	if d == nil {
		d = defaultDialer
	}
	c, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return -1, err
	}
	defer c.Close()
	if t, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(t)
		defer func() {
			_ = c.SetDeadline(time.Time{})
		}()
	}
	_, err = c.Write(q.Payload)
	if err != nil {
		return -1, err
	}
	return c.Read(buf)
}
