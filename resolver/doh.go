package resolver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

type ClientInfo struct {
	ID    string
	IP    string
	Model string
	Name  string
}

// DOH is a DNS over HTTPS implementation of the Resolver interface.
type DOH struct {
	// URL specifies the DoH upstream URL.
	URL string

	// GetURL provides a DoH upstream url for q. If GetURL is defined, URL is
	// ignored.
	GetURL func(q Query) string

	// ExtraHeaders specifies headers to be added to all DoH requests.
	ExtraHeaders http.Header

	// ClientInfo is called for each query in order gather client information to
	// embed with the request.
	ClientInfo func(Query) ClientInfo
}

func (r DOH) resolve(ctx context.Context, q Query, buf []byte, rt http.RoundTripper) (int, error) {
	var ci ClientInfo
	if r.ClientInfo != nil {
		ci = r.ClientInfo(q)
	}
	url := r.URL
	if r.GetURL != nil {
		url = r.GetURL(q)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(q.Payload))
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
	if ci.IP != "" {
		req.Header.Set("X-Device-Ip", ci.IP)
	}
	if ci.Model != "" {
		req.Header.Set("X-Device-Model", ci.Model)
	}
	if ci.Name != "" {
		req.Header.Set("X-Device-Name", ci.Name)
	}
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
