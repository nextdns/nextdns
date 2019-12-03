package endpoint

import (
	"context"
	"fmt"
	"net"

	"golang.org/x/net/dns/dnsmessage"
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

func (e *DNSEndpoint) Test(ctx context.Context, testDomain string) error {
	buf := make([]byte, 0, 514)
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{
		RecursionDesired: true,
	})
	err := b.StartQuestions()
	if err != nil {
		return fmt.Errorf("start question: %v", err)
	}
	err = b.Question(dnsmessage.Question{
		Class: dnsmessage.ClassINET,
		Type:  dnsmessage.TypeA,
		Name:  dnsmessage.MustNewName(testDomain),
	})
	if err != nil {
		return fmt.Errorf("question: %v", err)
	}
	buf, err = b.Finish()
	if err != nil {
		return fmt.Errorf("finish: %v", err)
	}
	d := &net.Dialer{}
	c, err := d.DialContext(ctx, "udp", e.Addr)
	if err != nil {
		return fmt.Errorf("dial: %v", err)
	}
	defer c.Close()
	if t, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(t)
	}
	_, err = c.Write(buf)
	if err != nil {
		return fmt.Errorf("write: %v", err)
	}
	_, err = c.Read(buf[:514])
	if err != nil {
		return fmt.Errorf("read: %v", err)
	}
	return nil
}
