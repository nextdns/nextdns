package proxy

import (
	"net"
	"sync"
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
		go func() {
			var err error
			var rsize int
			var qname string
			if p.QueryLog != nil {
				qname = lazyQName(buf[:qsize])
			}
			defer func() {
				bpool.Put(&buf)
				p.logQuery("udp", qname, qsize, rsize)
				p.logErr(err)
			}()
			res, err := p.resolve(buf[:qsize])
			if err != nil {
				return
			}
			if rsize, err = readDNSResponse(res, buf); err != nil {
				return
			}
			_, err = l.WriteTo(buf[:rsize], addr)
		}()
	}
}
