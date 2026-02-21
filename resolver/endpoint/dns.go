package endpoint

import (
	"context"
	"crypto/rand"
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
	if _, err := rand.Read(payload[:2]); err != nil {
		return 0, err
	}
	_, err = c.Write(payload)
	if err != nil {
		return 0, fmt.Errorf("write: %v", err)
	}
	id := uint16(payload[0])<<8 | uint16(payload[1])
	for {
		// DNS over UDP responses are at most 512 bytes, plus 2 bytes for the ID.
		// Keep backward-compat behavior but don't assume buf is always >= 514.
		readBuf := buf
		if len(readBuf) > 514 {
			readBuf = readBuf[:514]
		}
		if n, err = c.Read(readBuf); err != nil {
			return n, fmt.Errorf("read: %v", err)
		}
		if n < 2 {
			continue
		}
		if id != uint16(buf[0])<<8|uint16(buf[1]) {
			// Skip mismatch id as it may come from previous timeout query.
			continue
		}
		break
	}
	return
}
