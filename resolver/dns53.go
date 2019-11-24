package resolver

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DNS53 is a DNS53 implementation of the Resolver interface.
type DNS53 struct {
	Dialer *net.Dialer
}

var defaultDialer = &net.Dialer{}

func (r DNS53) resolve(ctx context.Context, q Query, buf []byte, addr string) (int, error) {
	d := r.Dialer
	if d == nil {
		d = defaultDialer
	}
	c, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return -1, fmt.Errorf("dial: %v", err)
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
		return -1, fmt.Errorf("write: %v", err)
	}
	n, err := c.Read(buf)
	if err != nil {
		return -1, fmt.Errorf("read: %v", err)
	}
	return n, nil
}
