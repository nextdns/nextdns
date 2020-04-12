package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/nextdns/nextdns/hosts"
	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

// QueryInfo provides information about a DNS query handled by Proxy.
type QueryInfo struct {
	Protocol          string
	PeerIP            net.IP
	Type              string
	Name              string
	QuerySize         int
	ResponseSize      int
	Duration          time.Duration
	FromCache         bool
	UpstreamTransport string
	Error             error
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

	// UseHosts specifies that /etc/hosts needs to be checked before calling the
	// upstream resolver.
	UseHosts bool

	// Timeout defines the maximum allowed time allowed for a request before
	// being cancelled.
	Timeout time.Duration

	// QueryLog specifies an optional log function called for each received query.
	QueryLog func(QueryInfo)

	// InfoLog specifies an option log function called when some actions are
	// performed.
	InfoLog func(string)

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

	var addrs []string

	// Try to lookup the given addr in the /etc/hosts file (for localhost for
	// instance).
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if ips := hosts.LookupHost(host); len(ips) > 0 {
			for _, ip := range ips {
				addrs = append(addrs, net.JoinHostPort(ip, port))
			}
		}
	}

	if len(addrs) == 0 {
		addrs = []string{addr}
	}

	lc := &net.ListenConfig{}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	expReturns := (len(addrs) * 2) + 1
	errs := make(chan error, expReturns)
	var closeAll []func() error

	for _, addr := range addrs {
		go func(addr string) {
			var err error
			p.logInfof("Listening on UDP/%s", addr)
			udp, err := lc.ListenPacket(ctx, "udp", addr)
			if err == nil {
				closeAll = append(closeAll, udp.Close)
				err = p.serveUDP(udp)
			}
			cancel()
			if err != nil {
				err = fmt.Errorf("udp: %w", err)
			}
			errs <- err
		}(addr)

		go func(addr string) {
			var err error
			p.logInfof("Listening on TCP/%s", addr)
			tcp, err := lc.Listen(ctx, "tcp", addr)
			if err == nil {
				closeAll = append(closeAll, tcp.Close)
				err = p.serveTCP(tcp)
			}
			cancel()
			if err != nil {
				err = fmt.Errorf("tcp: %w", err)
			}
			errs <- err
		}(addr)
	}

	<-ctx.Done()
	errs <- ctx.Err()
	for _, close := range closeAll {
		close()
	}
	// Wait for the two sockets (+ ctx err) to be terminated and return the
	// initial error.
	var err error
	for i := 0; i < expReturns; i++ {
		if e := <-errs; (err == nil || errors.Is(err, context.Canceled)) && e != nil {
			err = e
		}
	}
	if err != nil {
		return fmt.Errorf("proxy: %w", err)
	}
	return nil
}

func (p Proxy) Resolve(ctx context.Context, q query.Query, buf []byte) (n int, i resolver.ResolveInfo, err error) {
	if p.UseHosts {
		n, i, err = hostsResolve(q, buf)
		if err == nil {
			return
		}
	}
	if p.BogusPriv && q.Type == query.TypePTR && isPrivateReverse(q.Name) {
		return replyNXDomain(q, buf)
	}

	return p.Upstream.Resolve(ctx, q, buf)
}

func (p Proxy) logQuery(q QueryInfo) {
	if p.QueryLog != nil {
		p.QueryLog(q)
	}
}

func (p Proxy) logInfof(format string, a ...interface{}) {
	if p.InfoLog != nil {
		p.InfoLog(fmt.Sprintf(format, a...))
	}
}

func (p Proxy) logErr(err error) {
	if err != nil && p.ErrorLog != nil {
		p.ErrorLog(err)
	}
}
