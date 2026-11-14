package resolver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestDOHStaleCacheReturnsBeforeSlowUpstream(t *testing.T) {
	prev := staleRefreshWait
	staleRefreshWait = 30 * time.Millisecond
	t.Cleanup(func() { staleRefreshWait = prev })

	q, staleMsg := cachedTestQuery(t)
	original := append([]byte(nil), q.Payload...)
	cache := newMemCache()
	cache.m[cacheKey{"https://resolver.test/dns-query", q.Class, q.Type, q.Name}.Hash()] = &cacheValue{
		time:  time.Now().Add(-2 * time.Minute),
		msg:   staleMsg,
		trans: "HTTP/2.0",
	}
	fresh := append([]byte(nil), staleMsg...)
	fresh[len(fresh)-1] = 2

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFn := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFn)
	entered := make(chan struct{})
	var enteredOnce sync.Once
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(body, original) {
			t.Errorf("upstream saw a mutated query: %x", body)
		}
		return dnsResponse(fresh), nil
	})

	r := DOH{URL: "https://resolver.test/dns-query", Cache: cache}
	buf := make([]byte, 512)
	done := make(chan resolveResult, 1)
	go func() {
		n, info, err := r.resolve(context.Background(), q, buf, rt, newRequestLimiter(1))
		done <- resolveResult{n: n, info: info, err: err}
	}()
	select {
	case res := <-done:
		if res.err != nil || res.n == 0 || !res.info.FromCache || buf[res.n-1] != 1 {
			t.Fatalf("resolve() = n %d fromCache %v last %d err %v; want stale answer", res.n, res.info.FromCache, buf[res.n-1], res.err)
		}
	case <-time.After(time.Second):
		t.Fatal("expired cache waited for the slow upstream")
	}
	// The caller's query buffer is reusable once resolve returns. The refresh
	// must have kept its own copy.
	q.Payload[len(q.Payload)-1] ^= 0xff
	releaseFn()
	select {
	case <-cache.set:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not update the cache")
	}
	got, ok := cache.Get(cacheKey{"https://resolver.test/dns-query", q.Class, q.Type, q.Name}.Hash())
	if !ok || got.msg[len(got.msg)-1] != 2 {
		t.Fatal("cache was not replaced with the fresh answer")
	}
	select {
	case <-entered:
	default:
		t.Fatal("upstream was not contacted")
	}
}

func TestDOHStaleCachePrefersFastUpstream(t *testing.T) {
	prev := staleRefreshWait
	staleRefreshWait = 250 * time.Millisecond
	t.Cleanup(func() { staleRefreshWait = prev })

	q, staleMsg := cachedTestQuery(t)
	fresh := append([]byte(nil), staleMsg...)
	fresh[len(fresh)-1] = 2
	r := DOH{
		URL: "https://resolver.test/dns-query",
		Cache: staticCache{value: &cacheValue{
			time:  time.Now().Add(-2 * time.Minute),
			msg:   staleMsg,
			trans: "HTTP/2.0",
		}},
	}
	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return dnsResponse(fresh), nil
	})
	buf := make([]byte, 512)
	start := time.Now()
	n, info, err := r.resolve(context.Background(), q, buf, rt, newRequestLimiter(1))
	if err != nil || info.FromCache || n == 0 || buf[n-1] != 2 {
		t.Fatalf("resolve() = n %d fromCache %v last %d err %v; want fresh answer", n, info.FromCache, buf[n-1], err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("fast upstream took %v", time.Since(start))
	}
}

func TestDOHStaleCacheReturnedOnUpstreamError(t *testing.T) {
	prev := staleRefreshWait
	staleRefreshWait = time.Second
	t.Cleanup(func() { staleRefreshWait = prev })

	q, staleMsg := cachedTestQuery(t)
	r := DOH{
		URL: "https://resolver.test/dns-query",
		Cache: staticCache{value: &cacheValue{
			time:  time.Now().Add(-2 * time.Minute),
			msg:   staleMsg,
			trans: "HTTP/2.0",
		}},
	}
	upstreamErr := errors.New("connection reset")
	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, upstreamErr
	})
	buf := make([]byte, 512)
	start := time.Now()
	n, info, err := r.resolve(context.Background(), q, buf, rt, newRequestLimiter(1))
	if !errors.Is(err, upstreamErr) || n == 0 || !info.FromCache || buf[n-1] != 1 {
		t.Fatalf("resolve() = n %d fromCache %v err %v; want stale answer and upstream error", n, info.FromCache, err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("upstream error waited %v", time.Since(start))
	}
}

func TestDOHStaleCacheServedWhenUpstreamBusy(t *testing.T) {
	q, staleMsg := cachedTestQuery(t)
	l := newRequestLimiter(1)
	if err := l.tryAcquire(); err != nil {
		t.Fatal(err)
	}
	defer l.release()
	r := DOH{
		URL: "https://resolver.test/dns-query",
		Cache: staticCache{value: &cacheValue{
			time:  time.Now().Add(-2 * time.Minute),
			msg:   staleMsg,
			trans: "HTTP/2.0",
		}},
	}
	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("RoundTrip called after upstream admission was rejected")
		return nil, nil
	})
	buf := make([]byte, 512)
	n, info, err := r.resolve(context.Background(), q, buf, rt, l)
	if !errors.Is(err, ErrUpstreamBusy) || n == 0 || !info.FromCache {
		t.Fatalf("resolve() = n %d fromCache %v err %v; want stale answer", n, info.FromCache, err)
	}
}

func TestDOHNewerConfigurationSkipsStaleCache(t *testing.T) {
	prev := staleRefreshWait
	staleRefreshWait = time.Second
	t.Cleanup(func() { staleRefreshWait = prev })

	q, staleMsg := cachedTestQuery(t)
	fresh := append([]byte(nil), staleMsg...)
	fresh[len(fresh)-1] = 2
	url := "https://resolver.test/dns-query"
	release := make(chan struct{})
	r := DOH{
		URL:   url,
		Cache: staticCache{value: &cacheValue{time: time.Now().Add(-2 * time.Minute), msg: staleMsg}},
		lastModified: map[string]time.Time{
			url: time.Now().Add(time.Hour),
		},
	}
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return dnsResponse(fresh), nil
	})
	buf := make([]byte, 512)
	done := make(chan resolveResult, 1)
	go func() {
		n, info, err := r.resolve(context.Background(), q, buf, rt, newRequestLimiter(1))
		done <- resolveResult{n: n, info: info, err: err}
	}()
	select {
	case res := <-done:
		close(release)
		t.Fatalf("stale answer served after configuration change: n %d fromCache %v err %v", res.n, res.info.FromCache, res.err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case res := <-done:
		if res.err != nil || res.info.FromCache || res.n == 0 || buf[res.n-1] != 2 {
			t.Fatalf("resolve() = n %d fromCache %v last %d err %v; want fresh answer", res.n, res.info.FromCache, buf[res.n-1], res.err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh did not finish after release")
	}
}

type resolveResult struct {
	n    int
	info ResolveInfo
	err  error
}

type memCache struct {
	mu  sync.Mutex
	m   map[uint64]*cacheValue
	set chan struct{}
}

func newMemCache() *memCache {
	return &memCache{m: map[uint64]*cacheValue{}, set: make(chan struct{}, 4)}
}

func (c *memCache) Get(key uint64) (*cacheValue, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *memCache) Set(key uint64, value *cacheValue) {
	c.mu.Lock()
	c.m[key] = value
	c.mu.Unlock()
	select {
	case c.set <- struct{}{}:
	default:
	}
}

func dnsResponse(msg []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/2.0",
		Body:       io.NopCloser(bytes.NewReader(msg)),
		Header:     make(http.Header),
	}
}
