package proxy

import (
	"context"
	"net"
	"time"
)

type QueryInfo struct {
	Query        Query
	ResponseSize int
	Duration     time.Duration
}

type Resolver interface {
	Resolve(q Query, buf []byte) (int, error)
}

type Proxy struct {
	// Addr specifies the TCP/UDP address to listen to, :53 if empty.
	Addr string

	// Upstream specifies the resolver used for incoming queries.
	Upstream Resolver

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
		errs <- err
	}()

	go func() {
		var err error
		tcp, err = net.Listen("tcp", addr)
		if err == nil {
			err = p.serveTCP(tcp)
		}
		cancel()
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
	return err
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

func addrIP(addr net.Addr) (ip net.IP) {
	// Avoid parsing/alloc when it's an IP already.
	switch addr := addr.(type) {
	case *net.IPAddr:
		ip = addr.IP
	case *net.UDPAddr:
		ip = addr.IP
	case *net.TCPAddr:
		ip = addr.IP
	default:
		host, _, _ := net.SplitHostPort(addr.String())
		ip = net.ParseIP(host)
	}
	return
}
