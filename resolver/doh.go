package resolver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nextdns/nextdns/resolver/query"
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
	GetURL func(q query.Query) string

	// Cache defines the cache storage implementation for DNS response cache. If
	// nil, caching is disabled.
	Cache Cacher

	// ExtraHeaders specifies headers to be added to all DoH requests.
	ExtraHeaders http.Header

	// ClientInfo is called for each query in order gather client information to
	// embed with the request.
	ClientInfo func(query.Query) ClientInfo
}

// resolve perform the the DoH call.
func (r DOH) resolve(ctx context.Context, q query.Query, buf []byte, rt http.RoundTripper) (n int, i ResolveInfo, err error) {
	var ci ClientInfo
	if r.ClientInfo != nil {
		ci = r.ClientInfo(q)
	}
	url := r.URL
	if r.GetURL != nil {
		url = r.GetURL(q)
	}
	if url == "" {
		url = "https://0.0.0.0"
	}
	var now time.Time
	n = -1
	// RFC1035, section 7.4: The results of an inverse query should not be cached
	if q.Type != query.TypePTR && r.Cache != nil {
		now = time.Now()
		if v, found := r.Cache.Get(cacheKey{url, q.Class, q.Type, q.Name}); found {
			if v, ok := v.(*cacheValue); ok {
				msg, minTTL := v.AdjustedResponse(q.ID, now)
				copy(buf, msg)
				n = len(msg)
				i.Transport = v.trans
				i.FromCache = true
				if minTTL > 0 {
					return n, i, nil
				}
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(q.Payload))
	if err != nil {
		return n, i, err
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
		return n, i, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return n, i, fmt.Errorf("error code: %d", res.StatusCode)
	}
	n, err = readDNSResponse(res.Body, buf)
	i.Transport = res.Proto
	i.FromCache = false
	if r.Cache != nil {
		v := &cacheValue{
			time:  now,
			msg:   make([]byte, n),
			trans: res.Proto,
		}
		copy(v.msg, buf[:n])
		r.Cache.Add(cacheKey{url, q.Class, q.Type, q.Name}, v)
	}
	return n, i, err
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
