package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
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

type HostResolver interface {
	LookupAddr(addr string) []string
	LookupHost(addr string) []string
}

// Proxy is a DNS53 to DNS over anything proxy.
type Proxy struct {
	// Addrs specifies the TCP/UDP address to listen to, :53 if empty.
	Addrs []string

	// LocalResolver is called before the upstream to resolve local hostnames or
	// IPs.
	LocalResolver HostResolver

	// Upstream specifies the resolver used for incoming queries.
	Upstream resolver.Resolver

	// DiscoveryResolver is called after the upstream if no result was found.
	DiscoveryResolver HostResolver

	// BogusPriv specifies that reverse lookup on private subnets are answerd
	// with NXDOMAIN.
	BogusPriv bool

	// Timeout defines the maximum allowed time allowed for a request before
	// being cancelled.
	Timeout time.Duration

	// Maximum number of inflight requests. Further requests will
	// not be answered.
	MaxInflightRequests uint

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
	var addrs []string

	for _, addr := range p.Addrs {
		if addr == "" {
			addr = ":53"
		}

		// Try to lookup the given addr in the /etc/hosts file (for localhost for
		// instance).
		found := false
		if host, port, err := net.SplitHostPort(addr); err == nil {
			if ips := hosts.LookupHost(host); len(ips) > 0 {
				for _, ip := range ips {
					found = true
					addrs = append(addrs, net.JoinHostPort(ip, port))
				}
			}
		}
		if !found {
			addrs = append(addrs, addr)
		}
	}

	lc := &net.ListenConfig{}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	expReturns := (len(addrs) * 2) + 1
	errs := make(chan error, expReturns)
	var closeAll []func() error
	var closeAllMu sync.Mutex
	inflightRequests := make(chan struct{}, p.MaxInflightRequests)

	for _, addr := range addrs {
		go func(addr string) {
			var err error
			p.logInfof("Listening on UDP/%s", addr)
			udp, err := lc.ListenPacket(ctx, "udp", addr)
			if err == nil {
				closeAllMu.Lock()
				closeAll = append(closeAll, udp.Close)
				closeAllMu.Unlock()
				err = p.serveUDP(udp, inflightRequests)
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
				closeAllMu.Lock()
				closeAll = append(closeAll, tcp.Close)
				closeAllMu.Unlock()
				err = p.serveTCP(tcp, inflightRequests)
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
	if p.LocalResolver != nil {
		if _n, _i, _err := hostsResolve(p.LocalResolver, q, buf); _err == nil {
			return _n, _i, nil
		}
	}

	if !p.BogusPriv || q.Type != query.TypePTR || !isPrivateReverse(q.Name) {
		n, i, err = p.Upstream.Resolve(ctx, q, buf)
	}

	if q.RecursionDesired && p.DiscoveryResolver != nil && (n <= 0 || isNXDomain(buf[:n])) {
		if _n, _i, _err := hostsResolve(p.DiscoveryResolver, q, buf); _err == nil {
			return _n, _i, nil
		}
	}

	return n, i, err
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
