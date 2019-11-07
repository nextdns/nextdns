package proxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/mostlygeek/arp"
)

const maxTCPSize = 65535

func (p Proxy) serveTCP(l net.Listener) error {
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
			if err := p.serveTCPConn(c, bpool); err != nil {
				if p.ErrorLog != nil {
					p.ErrorLog(err)
				}
			}
		}()
	}
}

func (p Proxy) serveTCPConn(c net.Conn, bpool *sync.Pool) error {
	defer c.Close()

	for {
		buf := *bpool.Get().(*[]byte)
		qsize, err := readTCP(c, buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("TCP read: %v", err)
		}
		if qsize <= 14 {
			return fmt.Errorf("query too small: %d", qsize)
		}
		start := time.Now()
		go func() {
			var err error
			var rsize int
			ip := addrIP(c.RemoteAddr())
			q := Query{
				Protocol: "tcp",
				PeerIP:   ip,
				MAC:      parseMAC(arp.Search(ip.String())),
				Payload:  buf[:qsize],
			}
			if err := q.Parse(); err != nil {
				p.logErr(err)
			}
			var ci ClientInfo
			if p.ClientInfo != nil {
				ci = p.ClientInfo(q)
			}
			defer func() {
				bpool.Put(&buf)
				p.logQuery(QueryInfo{
					Query:        q,
					ClientInfo:   ci,
					ResponseSize: rsize,
					Duration:     time.Since(start),
				})
				p.logErr(err)
			}()
			res, err := p.resolve(q, ci)
			if err != nil {
				return
			}
			if rsize, err = readDNSResponse(res, buf); err != nil {
				return
			}
			err = writeTCP(c, buf[:rsize])
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
