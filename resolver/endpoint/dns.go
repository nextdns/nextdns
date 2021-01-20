package endpoint

import (
	"context"
	"fmt"
	"net"
)

type DNSEndpoint struct {
	// Addr use to contact the DNS server.
	Addr string
}

func (e *DNSEndpoint) Protocol() Protocol {
	return ProtocolDNS
}

func (e *DNSEndpoint) Equal(e2 Endpoint) bool {
	if e2, ok := e2.(*DNSEndpoint); ok {
		return e.Addr == e2.Addr
	}
	return false
}

func (e *DNSEndpoint) String() string {
	return e.Addr
}

func (e *DNSEndpoint) Exchange(ctx context.Context, payload, buf []byte) (n int, err error) {
	d := &net.Dialer{}
	c, err := d.DialContext(ctx, "udp", e.Addr)
	if err != nil {
		return 0, fmt.Errorf("dial: %v", err)
	}
	defer c.Close()
	if t, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(t)
	}
	_, err = c.Write(buf)
	if err != nil {
		return 0, fmt.Errorf("write: %v", err)
	}
	n, err = c.Read(buf)
	if err != nil {
		return n, fmt.Errorf("read: %v", err)
	}
	return
}
