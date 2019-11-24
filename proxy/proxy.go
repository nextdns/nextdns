package proxy

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/nextdns/nextdns/resolver"
)

// QueryInfo provides information about a DNS query handled by Proxy.
type QueryInfo struct {
	Protocol     string
	PeerIP       net.IP
	Name         string
	QuerySize    int
	ResponseSize int
	Duration     time.Duration
}

// Proxy is a DNS53 to DNS over anything proxy.
type Proxy struct {
	// Addr specifies the TCP/UDP address to listen to, :53 if empty.
	Addr string

	// Upstream specifies the resolver used for incoming queries.
	Upstream resolver.Resolver

	// BogusPriv specifies that reverse lookup on private subnets are answerd
	// with NXDOMAIN.
	BogusPriv bool

	// Timeout defines the maximum allowed time allowed for a request before
	// being cancelled.
	Timeout time.Duration

	// QueryLog specifies an optional log function called for each received query.
	QueryLog func(QueryInfo)

	// ErrorLog specifies an optional log function for errors. If not set,
	// errors are not reported.
	ErrorLog func(error)
}

// ListenAndServe listens on UDP and TCP and serve DNS queries. If ctx is
// canceled, listeners are closed and ListenAndServe returns context.Canceled
// error.
func (p Proxy) ListenAndServe(ctx context.Context) error {
	addr := p.Addr
	if addr == "" {
		addr = ":53"
	}

	var udp net.PacketConn
	var tcp net.Listener
	ctx, cancel := context.WithCancel(ctx)
	errs := make(chan error, 3)

	go func() {
		var err error
		udp, err = net.ListenPacket("udp", addr)
		if err == nil {
			err = p.serveUDP(udp)
		}
		cancel()
		if err != nil {
			err = fmt.Errorf("udp: %v", err)
		}
		errs <- err
	}()

	go func() {
		var err error
		tcp, err = net.Listen("tcp", addr)
		if err == nil {
			err = p.serveTCP(tcp)
		}
		cancel()
		if err != nil {
			err = fmt.Errorf("tcp: %v", err)
		}
		errs <- err
	}()

	<-ctx.Done()
	errs <- ctx.Err()
	if udp != nil {
		udp.Close()
	}
	if tcp != nil {
		tcp.Close()
	}
	// Wait for the two sockets (+ ctx err) to be terminated and return the
	// initial error.
	var err error
	for i := 0; i < 3; i++ {
		if e := <-errs; err == nil && e != nil {
			err = e
		}
	}
	if err != nil {
		return fmt.Errorf("proxy: %v", err)
	}
	return nil
}

func (p Proxy) Resolve(ctx context.Context, q resolver.Query, buf []byte) (n int, err error) {
	if p.BogusPriv && isPrivateReverse(q.Name) {
		return replyNXDomain(q, buf)
	}
	return p.Upstream.Resolve(ctx, q, buf)
}

func (p Proxy) logQuery(q QueryInfo) {
	if p.QueryLog != nil {
		p.QueryLog(q)
	}
}

func (p Proxy) logErr(err error) {
	if err != nil && p.ErrorLog != nil {
		p.ErrorLog(err)
	}
}
