package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

const (
	maxUDPSize  = 512
	maxDNS0Size = 4094
)

// This is the required size of the OOB buffer to pass to ReadMsgUDP.
var udpOOBSize = func() int {
	// We can't know whether we'll get an IPv4 control message or an
	// IPv6 control message ahead of time. To get around this, we size
	// the buffer equal to the largest of the two.

	oob4 := ipv4.NewControlMessage(ipv4.FlagDst | ipv4.FlagInterface)
	oob6 := ipv6.NewControlMessage(ipv6.FlagDst | ipv6.FlagInterface)

	if len(oob4) > len(oob6) {
		return len(oob4)
	}

	return len(oob6)
}()

func (p Proxy) serveUDP(l net.PacketConn, inflightRequests chan struct{}) error {
	bpool := sync.Pool{
		New: func() interface{} {
			// Use the same buffer size as for TCP and truncate later. UDP and
			// TCP share the cache, and we want to avoid storing truncated
			// response for UDP that would be reused when the client falls back
			// to TCP.
			b := make([]byte, maxTCPSize)
			return &b
		},
	}

	c, ok := l.(*net.UDPConn)
	if !ok {
		return errors.New("not a UDP socket")
	}
	if err := setUDPDstOptions(c); err != nil {
		return fmt.Errorf("setUDPDstOptions: %w", err)
	}

	for {
		inflightRequests <- struct{}{}
		buf := *bpool.Get().(*[]byte)
		qsize, lip, raddr, err := readUDP(c, buf)
		if err != nil {
			<-inflightRequests
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				bpool.Put(&buf)
				continue
			}
			return err
		}
		if qsize <= 14 {
			bpool.Put(&buf)
			<-inflightRequests
			continue
		}
		start := time.Now()
		go func() {
			var err error
			var rsize int
			var ri resolver.ResolveInfo
			q, err := query.New(buf[:qsize], addrIP(raddr))
			if err != nil {
				p.logErr(err)
			}
			rbuf := *bpool.Get().(*[]byte)
			defer func() {
				if r := recover(); r != nil {
					stackBuf := make([]byte, 64<<10)
					stackBuf = stackBuf[:runtime.Stack(stackBuf, false)]
					err = fmt.Errorf("panic: %v: %s", r, string(stackBuf))
				}
				bpool.Put(&buf)
				bpool.Put(&rbuf)
				<-inflightRequests
				p.logQuery(QueryInfo{
					PeerIP:            q.PeerIP,
					Protocol:          "UDP",
					Type:              q.Type.String(),
					Name:              q.Name,
					QuerySize:         qsize,
					ResponseSize:      rsize,
					Duration:          time.Since(start),
					FromCache:         ri.FromCache,
					UpstreamTransport: ri.Transport,
					Error:             err,
				})
			}()
			ctx := context.Background()
			if p.Timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, p.Timeout)
				defer cancel()
			}
			if rsize, ri, err = p.Resolve(ctx, q, rbuf); err != nil || rsize <= 0 || rsize > maxTCPSize {
				rsize = replyServFail(q, rbuf)
			}
			if rsize > maxUDPSize && (rsize > int(q.MsgSize) || rsize > maxDNS0Size) {
				if q.MsgSize > maxUDPSize {
					if rsize > int(q.MsgSize) {
						rsize = int(q.MsgSize)
					}
				} else {
					rsize = maxUDPSize
				}
				rbuf[2] |= 0x2 // mark response as truncated
			}
			_, _, werr := c.WriteMsgUDP(rbuf[:rsize], oobWithSrc(lip), raddr)
			if err == nil {
				// Do not overwrite resolve error when on cache fallback.
				err = werr
			}
		}()
	}
}

// readUDP reads from c to buf and returns the local and remote addresses.
func readUDP(c *net.UDPConn, buf []byte) (n int, lip net.IP, raddr *net.UDPAddr, err error) {
	var oobn int
	oob := make([]byte, udpOOBSize)
	n, oobn, _, raddr, err = c.ReadMsgUDP(buf, oob)
	if err != nil {
		return -1, nil, nil, err
	}
	lip = parseDstFromOOB(oob[:oobn])
	return n, lip, raddr, nil
}

// oobWithSrc returns oob data with the Dst set with ip.
func oobWithSrc(ip net.IP) []byte {
	// If the dst is definitely an IPv6, then use ipv6's ControlMessage to
	// respond otherwise use ipv4's because ipv6's marshal ignores ipv4
	// addresses.
	if ip.To4() == nil {
		cm := &ipv6.ControlMessage{}
		cm.Src = ip
		return cm.Marshal()
	}
	cm := &ipv4.ControlMessage{}
	cm.Src = ip
	return cm.Marshal()
}
