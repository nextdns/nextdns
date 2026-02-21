package proxy

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/nextdns/nextdns/internal/dnsmessage"
	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

// serveDoT accepts TLS connections on l and handles DNS queries using the same
// wire format as DNS-over-TCP (2-byte length prefix + DNS message). The only
// difference from serveTCP is the protocol label logged as "DoT".
func (p Proxy) serveDoT(l net.Listener, inflightRequests chan struct{}) error {
	bpool := &sync.Pool{
		New: func() interface{} {
			return make([]byte, maxTCPSize)
		},
	}

	for {
		c, err := l.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return err
		}
		go func() {
			if err := p.serveDoTConn(c, inflightRequests, bpool); err != nil {
				if p.ErrorLog != nil {
					p.ErrorLog(err)
				}
			}
		}()
	}
}

func (p Proxy) serveDoTConn(c net.Conn, inflightRequests chan struct{}, bpool *sync.Pool) error {
	defer c.Close()

	for {
		inflightRequests <- struct{}{}
		buf := bpool.Get().([]byte)
		qsize, err := readTCP(c, buf)
		if err != nil {
			<-inflightRequests
			if err.Error() == "EOF" {
				return nil
			}
			return fmt.Errorf("DoT read: %v", err)
		}
		if qsize <= 14 {
			<-inflightRequests
			return fmt.Errorf("query too small: %d", qsize)
		}
		start := time.Now()
		go func() {
			var err error
			var rsize int
			var ri resolver.ResolveInfo
			localIP := addrIP(c.LocalAddr())
			remoteIP := addrIP(c.RemoteAddr())
			q, err := query.New(buf[:qsize], remoteIP, localIP)
			if err != nil {
				p.logErr(err)
			}
			rbuf := bpool.Get().([]byte)
			defer func() {
				if r := recover(); r != nil {
					stackBuf := make([]byte, 64<<10)
					stackBuf = stackBuf[:runtime.Stack(stackBuf, false)]
					err = fmt.Errorf("panic: %v: %s", r, string(stackBuf))
				}
				bpool.Put(buf)
				bpool.Put(rbuf)
				<-inflightRequests
				p.logQuery(QueryInfo{
					PeerIP:            q.PeerIP,
					Protocol:          "DoT",
					Type:              q.Type.String(),
					Name:              q.Name,
					QuerySize:         qsize,
					ResponseSize:      rsize,
					Duration:          time.Since(start),
					Profile:           ri.Profile,
					FromCache:         ri.FromCache,
					UpstreamTransport: ri.Transport,
					Error:             err,
				})
			}()

			if err != nil {
				rsize = replyRCode(dnsmessage.RCodeFormatError, q, rbuf)
				werr := writeTCP(c, rbuf[:rsize])
				if werr != nil {
					err = fmt.Errorf("%v (write: %w)", err, werr)
				}
				return
			}
			ctx := context.Background()
			if p.Timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, p.Timeout)
				defer cancel()
			}
			if rsize, ri, err = p.Resolve(ctx, q, rbuf); err != nil || rsize <= 0 || rsize > maxTCPSize {
				rsize = replyRCode(dnsmessage.RCodeServerFailure, q, rbuf)
			}
			werr := writeTCP(c, rbuf[:rsize])
			if err == nil {
				err = werr
			}
		}()
	}
}
