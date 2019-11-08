package doh

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/nextdns/nextdns/proxy"
)

type ClientInfo struct {
	ID    string
	Model string
	Name  string
}

type Resolver struct {
	// Upstream specifies the DoH upstream URL for q.
	URL func(q proxy.Query) string

	// Transport specifies the http.RoundTripper to use to contact upstream. If
	// nil, the default is http.DefaultTransport.
	Transport http.RoundTripper

	// ExtraHeaders specifies headers to be added to all DoH requests.
	ExtraHeaders http.Header

	// ClientInfo is called for each query in order gather client information to
	// embed with the request.
	ClientInfo func(proxy.Query) ClientInfo
}

func (r Resolver) Resolve(q proxy.Query, buf []byte) (int, error) {
	var ci ClientInfo
	if r.ClientInfo != nil {
		ci = r.ClientInfo(q)
	}
	req, err := http.NewRequest("POST", r.URL(q), bytes.NewReader(q.Payload))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	for name, values := range r.ExtraHeaders {
		req.Header[name] = values
	}
	if ci.ID != "" {
		req.Header.Set("X-Device-Id", ci.ID)
	}
	if ci.Model != "" {
		req.Header.Set("X-Device-Model", ci.Model)
	}
	if ci.Name != "" {
		req.Header.Set("X-Device-Name", ci.Name)
	}
	rt := r.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	res, err := rt.RoundTrip(req)
	if err != nil {
		return -1, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("error code: %d", res.StatusCode)
	}
	return readDNSResponse(res.Body, buf)
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
