package proxy

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/nextdns/nextdns/resolver"
)

const maxUDPSize = 512

func (p Proxy) serveUDP(l net.PacketConn) error {
	bpool := sync.Pool{
		New: func() interface{} {
			b := make([]byte, maxUDPSize)
			return &b
		},
	}

	for {
		buf := *bpool.Get().(*[]byte)
		qsize, addr, err := l.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				bpool.Put(&buf)
				continue
			}
			return err
		}
		if qsize <= 14 {
			bpool.Put(&buf)
			continue
		}
		start := time.Now()
		go func() {
			var err error
			var rsize int
			var ri resolver.ResolveInfo
			ip := addrIP(addr)
			q, err := resolver.NewQuery(buf[:qsize], ip)
			if err != nil {
				p.logErr(err)
			}
			defer func() {
				bpool.Put(&buf)
				p.logQuery(QueryInfo{
					PeerIP:            q.PeerIP,
					Protocol:          "UDP",
					Type:              q.Type,
					Name:              q.Name,
					QuerySize:         qsize,
					ResponseSize:      rsize,
					Duration:          time.Since(start),
					UpstreamTransport: ri.Transport,
				})
				p.logErr(err)
			}()
			ctx := context.Background()
			if p.Timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, p.Timeout)
				defer cancel()
			}
			if rsize, ri, err = p.Resolve(ctx, q, buf); err != nil {
				return
			}
			if rsize > maxUDPSize {
				return
			}
			_, err = l.WriteTo(buf[:rsize], addr)
		}()
	}
}
