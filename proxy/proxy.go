package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type QueryInfo struct {
	Protocol     string
	Name         string
	QuerySize    int
	ResponseSize int
	Duration     time.Duration
}

type Proxy struct {
	// Addr specifies the TCP/UDP address to listen to, :53 if empty.
	Addr string

	// Upstream specifies the DoH upstream URL.
	Upstream string

	// Transport specifies the http.RoundTripper to use to contact upstream. If
	// nil, the default is http.DefaultTransport.
	Transport http.RoundTripper

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

func (p Proxy) resolve(buf []byte) (io.ReadCloser, error) {
	req, err := http.NewRequest("POST", p.Upstream, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	rt := p.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	res, err := rt.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error code: %d", res.StatusCode)
	}
	return res.Body, nil
}

func readDNSResponse(r io.Reader, buf []byte) (int, error) {
	var n int
	for {
		nn, err := r.Read(buf[n:])
		n += nn
		if err != nil {
			if err == io.EOF {
				break
			}
			return -1, err
		}
		if n >= len(buf) {
			buf[2] |= 0x2 // mark response as truncated
			break
		}
	}
	return n, nil
}

// lazyQName parses the qname from a DNS query without trying to parse or
// validate the whole query.
func lazyQName(buf []byte) string {
	qn := &strings.Builder{}
	for n := 12; n <= len(buf) && buf[n] != 0; {
		end := n + 1 + int(buf[n])
		if end > len(buf) {
			// invalid qname, stop parsing
			break
		}
		qn.Write(buf[n+1 : end])
		qn.WriteByte('.')
		n = end
	}
	return qn.String()
}
