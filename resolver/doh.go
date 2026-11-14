package resolver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
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

	// GetProfileURL provides a DoH upstream url for q. If GetProfileURL is
	// defined, URL is ignored.
	GetProfileURL func(q query.Query) (url, profile string)

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

// staleRefreshWait bounds how long an expired cache entry waits for a fresh
// upstream answer before being returned. UniFi access points record a client
// association as a DNS timeout when the lookup does not complete in roughly
// 1.5s, while a slow DoH refresh (anonymized EDNS client subnet, CNAME
// flattening, or a stalled HTTP/2 connection) is allowed to run until the
// request timeout, which defaults to 5s. A fast upstream still replaces the
// cached record; a slow one is refreshed in the background.
var staleRefreshWait = 250 * time.Millisecond

// resolve perform the the DoH call.
func (r *DOH) resolve(ctx context.Context, q query.Query, buf []byte, rt http.RoundTripper, limiter *requestLimiter) (n int, i ResolveInfo, err error) {
	var ci ClientInfo
	if r.ClientInfo != nil {
		ci = r.ClientInfo(q)
	}
	url := r.URL
	if r.GetProfileURL != nil {
		url, i.Profile = r.GetProfileURL(q)
	}
	if url == "" {
		url = "https://0.0.0.0"
	}
	var now time.Time
	n = 0
	// RFC1035, section 7.4: The results of an inverse query should not be cached
	if q.Type != query.TypePTR && r.Cache != nil {
		now = time.Now()
		k := cacheKey{url, q.Class, q.Type, q.Name}
		if v, found := r.Cache.Get(k.Hash()); found && v != nil && k.ValidateQuestion(v.msg) {
			var minTTL uint32
			n, minTTL = v.AdjustedResponse(buf, q.ID, r.CacheMaxAge, r.MaxTTL, now)
			// Use a cached entry only when it is not older than the
			// configuration last change. A newer configuration must not be
			// served from the previous answer, including on upstream failure.
			if r.lastMod(url).Before(v.time) {
				i.Transport = v.trans
				i.FromCache = true
				if minTTL > 0 {
					return n, i, nil
				}
			} else {
				n = 0
			}
		}
	}
	stale := n > 0 && i.FromCache
	if !stale {
		i.Transport = ""
		i.FromCache = false
		n = 0
	}
	if rt == nil {
		rt = http.DefaultTransport
	}
	if err := limiter.tryAcquire(); err != nil {
		if stale {
			// The previous answer is still the best one we can give without
			// waiting. Admission failure must not turn it into SERVFAIL.
			return n, i, err
		}
		return 0, i, err
	}
	if !stale {
		defer limiter.release()
		return r.exchange(ctx, q, q.Payload, buf, rt, ci, url, i.Profile, now)
	}

	// Keep refreshing after the caller is answered. Cancelling with the
	// client would drop the update that the next lookup needs.
	refreshCtx, refreshCancel := detachRefreshContext(ctx)
	payload := append([]byte(nil), q.Payload...)
	own := make([]byte, len(buf))
	staleN, staleI := n, i
	done := make(chan exchangeResult, 1)
	go func() {
		defer limiter.release()
		defer refreshCancel()
		nn, ii, err := r.exchange(refreshCtx, q, payload, own, rt, ci, url, staleI.Profile, now)
		done <- exchangeResult{n: nn, i: ii, err: err, buf: own}
	}()

	timer := time.NewTimer(staleRefreshWait)
	defer timer.Stop()
	select {
	case res := <-done:
		if res.err != nil || res.n <= 0 {
			return staleN, staleI, res.err
		}
		copy(buf, res.buf[:res.n])
		return res.n, res.i, nil
	case <-timer.C:
		return staleN, staleI, nil
	case <-ctx.Done():
		return staleN, staleI, ctx.Err()
	}
}

type exchangeResult struct {
	n   int
	i   ResolveInfo
	err error
	buf []byte
}

func detachRefreshContext(ctx context.Context) (context.Context, context.CancelFunc) {
	budget := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			budget = remaining
		}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), budget)
}

func (r *DOH) exchange(ctx context.Context, q query.Query, payload, buf []byte, rt http.RoundTripper, ci ClientInfo, url, profile string, now time.Time) (n int, i ResolveInfo, err error) {
	i.Profile = profile
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return 0, i, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("X-Conf-Last-Modified", "true")
	maps.Copy(req.Header, r.ExtraHeaders)
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
	res, err := rt.RoundTrip(req)
	if err != nil {
		return 0, i, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, i, fmt.Errorf("error code: %d", res.StatusCode)
	}
	var truncated bool
	n, truncated, err = readDNSResponse(res.Body, buf)
	i.Transport = res.Proto
	i.FromCache = false
	if q.Type != query.TypePTR && n > 0 && !truncated && err == nil && r.Cache != nil {
		if now.IsZero() {
			now = time.Now()
		}
		v := &cacheValue{
			time:  now,
			msg:   make([]byte, n),
			trans: res.Proto,
		}
		copy(v.msg, buf[:n])
		r.Cache.Set(cacheKey{url, q.Class, q.Type, q.Name}.Hash(), v)
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
