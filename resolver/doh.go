package resolver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
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

	// CacheMaxAge defines the maximum age in second allowed for a cached entry
	// before being considered stale regardless of the records TTL.
	CacheMaxAge uint32

	// MaxTTL defines the maximum TTL value that will be handed out to clients.
	// The specified maximum TTL will be given to clients instead of the true
	// TTL value if it is lower. The true TTL value is however kept in the cache
	// to evaluate cache entries freshness.
	MaxTTL uint32

	// ExtraHeaders specifies headers to be added to all DoH requests.
	ExtraHeaders http.Header

	// ClientInfo is called for each query in order gather client information to
	// embed with the request.
	ClientInfo func(query.Query) ClientInfo

	mu           sync.RWMutex
	lastModified map[string]time.Time // per URL last conf last modified
}

// resolve perform the the DoH call.
func (r *DOH) resolve(ctx context.Context, q query.Query, buf []byte, rt http.RoundTripper) (n int, i ResolveInfo, err error) {
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
	n = 0
	// RFC1035, section 7.4: The results of an inverse query should not be cached
	if q.Type != query.TypePTR && r.Cache != nil {
		now = time.Now()
		if v, found := r.Cache.Get(cacheKey{url, q.Class, q.Type, q.Name}); found {
			if v, ok := v.(*cacheValue); ok {
				var minTTL uint32
				n, minTTL = v.AdjustedResponse(buf, q.ID, r.CacheMaxAge, r.MaxTTL, now)
				i.Transport = v.trans
				i.FromCache = true
				// Use cached entry if TTL is in the future and isn't older than
				// the configuration last change.
				if minTTL > 0 && r.lastMod(url).Before(v.time) {
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
	req.Header.Set("X-Conf-Last-Modified", "true")
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
	var truncated bool
	n, truncated, err = readDNSResponse(res.Body, buf)
	i.Transport = res.Proto
	i.FromCache = false
	if n > 0 && !truncated && err == nil && r.Cache != nil {
		v := &cacheValue{
			time:  now,
			msg:   make([]byte, n),
			trans: res.Proto,
		}
		copy(v.msg, buf[:n])
		r.Cache.Add(cacheKey{url, q.Class, q.Type, q.Name}, v)
		r.updateLastMod(url, res.Header.Get("X-Conf-Last-Modified"))
	}
	if r.MaxTTL > 0 && n > 0 {
		updateTTL(buf[:n], 0, 0, r.MaxTTL)
	}
	return n, i, err
}

// lastMod returns the last modification time of the configuration pointed by
// url.
func (r *DOH) lastMod(url string) time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastModified[url]
}

// updateLastMod updates the last modification time of the configuration pointed
// by url if more recent than the one currently stored.
func (r *DOH) updateLastMod(url, lastMod string) {
	lastModTime, err := time.Parse(time.RFC1123, lastMod)
	if err != nil {
		return
	}
	r.mu.RLock()
	curLastModTime := r.lastModified[url]
	if !lastModTime.After(curLastModTime) {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()
	r.mu.Lock()
	curLastModTime = r.lastModified[url]
	if lastModTime.After(curLastModTime) {
		if r.lastModified == nil {
			r.lastModified = map[string]time.Time{}
		}
		r.lastModified[url] = lastModTime
	}
	r.mu.Unlock()
}

func readDNSResponse(r io.Reader, buf []byte) (n int, truncated bool, err error) {
	for {
		nn, err := r.Read(buf[n:])
		n += nn
		if err != nil {
			if err == io.EOF {
				break
			}
			return 0, false, err
		}
		if n >= len(buf) {
			buf[2] |= 0x2 // mark response as truncated
			truncated = true
			n = len(buf)
			break
		}
	}
	return n, truncated, nil
}
