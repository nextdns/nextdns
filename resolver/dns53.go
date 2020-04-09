package resolver

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/nextdns/nextdns/resolver/query"
)

// DNS53 is a DNS53 implementation of the Resolver interface.
type DNS53 struct {
	Dialer *net.Dialer

	// Cache defines the cache storage implementation for DNS response cache. If
	// nil, caching is disabled.
	Cache Cacher

	// CacheMaxTTL defines the maximum age in second allowed for a cached entry
	// before being considered stale regardless of the records TTL.
	CacheMaxTTL uint32
}

var defaultDialer = &net.Dialer{}

func (r DNS53) resolve(ctx context.Context, q query.Query, buf []byte, addr string) (n int, i ResolveInfo, err error) {
	i.Transport = "UDP"
	var now time.Time
	n = -1
	// RFC1035, section 7.4: The results of an inverse query should not be cached
	if q.Type != query.TypePTR && r.Cache != nil {
		now = time.Now()
		if v, found := r.Cache.Get(cacheKey{"", q.Class, q.Type, q.Name}); found {
			if v, ok := v.(*cacheValue); ok {
				msg, minTTL := v.AdjustedResponse(q.ID, r.CacheMaxTTL, now)
				copy(buf, msg)
				n = len(msg)
				i.FromCache = true
				if minTTL > 0 {
					return n, i, nil
				}
			}
		}
	}
	d := r.Dialer
	if d == nil {
		d = defaultDialer
	}
	c, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return n, i, fmt.Errorf("dial: %v", err)
	}
	defer c.Close()
	if t, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(t)
		defer func() {
			_ = c.SetDeadline(time.Time{})
		}()
	}
	_, err = c.Write(q.Payload)
	if err != nil {
		return n, i, fmt.Errorf("write: %v", err)
	}
	n, err = c.Read(buf)
	if err != nil {
		return n, i, fmt.Errorf("read: %v", err)
	}
	i.FromCache = false
	if r.Cache != nil {
		v := &cacheValue{
			time: now,
			msg:  make([]byte, n),
		}
		copy(v.msg, buf[:n])
		r.Cache.Add(cacheKey{"", q.Class, q.Type, q.Name}, v)
	}
	return n, i, nil
}
