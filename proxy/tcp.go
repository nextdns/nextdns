package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

const maxTCPSize = 65535

func (p Proxy) serveTCP(l net.Listener, inflightRequests chan struct{}) error {
	bpool := &sync.Pool{
		New: func() interface{} {
			b := make([]byte, maxTCPSize)
			return &b
		},
	}

	for {
		c, err := l.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				continue
			}
			return err
		}
		go func() {
			if err := p.serveTCPConn(c, inflightRequests, bpool); err != nil {
				if p.ErrorLog != nil {
					p.ErrorLog(err)
				}
			}
		}()
	}
}

func (p Proxy) serveTCPConn(c net.Conn, inflightRequests chan struct{}, bpool *sync.Pool) error {
	defer c.Close()

	for {
		inflightRequests <- struct{}{}
		buf := *bpool.Get().(*[]byte)
		qsize, err := readTCP(c, buf)
		if err != nil {
			<-inflightRequests
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("TCP read: %v", err)
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
			ip := addrIP(c.RemoteAddr())
			q, err := query.New(buf[:qsize], ip)
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
					Protocol:          "TCP",
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
			werr := writeTCP(c, rbuf[:rsize])
			if err == nil {
				// Do not overwrite resolve error when on cache fallback.
				err = werr
			}
		}()
	}
}

func readTCP(r io.Reader, buf []byte) (int, error) {
	var length uint16
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return -1, err
	}
	if length > maxTCPSize {
		return -1, errors.New("message too large")
	}
	return io.ReadFull(r, buf[:length])
}

func writeTCP(c net.Conn, buf []byte) error {
	if err := binary.Write(c, binary.BigEndian, uint16(len(buf))); err != nil {
		return err
	}
	_, err := c.Write(buf)
	return err
}
