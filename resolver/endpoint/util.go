package endpoint

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"golang.org/x/net/dns/dnsmessage"
)

func testDNS(ctx context.Context, testDomain, addr string) error {
	buf := make([]byte, 0, 514)
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{
		RecursionDesired: true,
	})
	_ = b.StartQuestions()
	_ = b.Question(dnsmessage.Question{
		Class: dnsmessage.ClassINET,
		Type:  dnsmessage.TypeA,
		Name:  dnsmessage.MustNewName(testDomain),
	})
	buf, _ = b.Finish()
	d := &net.Dialer{}
	c, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if t, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(t)
	}
	_, err = c.Write(buf)
	if err != nil {
		return err
	}
	_, err = c.Read(buf)
	return err
}

func testDOH(ctx context.Context, testDomain string, t http.RoundTripper) error {
	req, _ := http.NewRequest("GET", "https://nowhere?name="+testDomain, nil)
	req = req.WithContext(ctx)
	res, err := t.RoundTrip(req)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status: %d", res.StatusCode)
	}
	return nil
}
