package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

type Proxy struct {
	// Addr specifies the TCP/UDP address to listen to, :53 if empty.
	Addr string

	// Upstream specifies the DoH upstream URL.
	Upstream string

	// Client specifies the http client to use to communicate with the upstream.
	Client *http.Client

	// QueryLog specifies an optional log function called for each received query.
	QueryLog func(proto, name string, qsize, rsize int)

	// ErrorLog specifies an optional log function for errors. If not set,
	// errors are not reported.
	ErrorLog func(error)
}

func (p Proxy) ListenAndServe() (err error) {
	addr := p.Addr
	if addr == "" {
		addr = ":53"
	}

	wg := &sync.WaitGroup{}
	wg.Add(2)

	var udp net.PacketConn
	var tcp net.Listener

	go func() {
		defer wg.Done()
		udp, err = net.ListenPacket("udp", addr)
		if err == nil {
			err = p.serveUDP(udp)
		}
	}()

	go func() {
		defer wg.Done()
		tcp, err = net.Listen("tcp", addr)
		if err == nil {
			err = p.serveTCP(tcp)
		}
	}()

	wg.Wait()
	if udp != nil {
		udp.Close()
	}
	if tcp != nil {
		tcp.Close()
	}
	return err
}

func (p Proxy) logQuery(proto, qname string, qsize, rsize int) {
	if p.QueryLog != nil {
		p.QueryLog(proto, qname, qsize, rsize)
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
	res, err := p.Client.Do(req)
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
