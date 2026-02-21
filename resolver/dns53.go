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

	// CacheMaxAge defines the maximum age in second allowed for a cached entry
	// before being considered stale regardless of the records TTL.
	CacheMaxAge uint32

	// MaxTTL defines the maximum TTL value that will be handed out to clients.
	// The specified maximum TTL will be given to clients instead of the true
	// TTL value if it is lower. The true TTL value is however kept in the cache
	// to evaluate cache entries freshness.
	MaxTTL uint32
}

var defaultDialer = &net.Dialer{}

func (r DNS53) resolve(ctx context.Context, q query.Query, buf []byte, addr string) (n int, i ResolveInfo, err error) {
	i.Transport = "UDP"
	var now time.Time
	n = 0
	// RFC1035, section 7.4: The results of an inverse query should not be cached
	if q.Type != query.TypePTR && r.Cache != nil {
		now = time.Now()
		k := cacheKey{"", q.Class, q.Type, q.Name}
		if v, found := r.Cache.Get(k.Hash()); found && v != nil && k.ValidateQuestion(v.msg) {
			var minTTL uint32
			n, minTTL = v.AdjustedResponse(buf, q.ID, r.CacheMaxAge, r.MaxTTL, now)
			i.FromCache = true
			if minTTL > 0 {
				return n, i, nil
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
	}
	_, err = c.Write(q.Payload)
	if err != nil {
		return n, i, fmt.Errorf("write: %v", err)
	}
	for {
		if n, err = c.Read(buf); err != nil {
			return n, i, fmt.Errorf("read: %v", err)
		}
		if n < 2 {
			continue
		}
		if q.ID != uint16(buf[0])<<8|uint16(buf[1]) {
			// Skip mismatch id as it may come from previous timeout query.
			continue
		}
		break
	}
	i.FromCache = false
	if q.Type != query.TypePTR && r.Cache != nil {
		v := &cacheValue{
			time: now,
			msg:  make([]byte, n),
		}
		copy(v.msg, buf[:n])
		k := cacheKey{"", q.Class, q.Type, q.Name}
		if bc, ok := r.Cache.(*ByteCache); ok {
			bc.SetTracked(k.Hash(), v, "")
		} else {
			r.Cache.Set(k.Hash(), v)
		}
	}
	if r.MaxTTL > 0 {
		updateTTL(buf[:n], 0, 0, r.MaxTTL)
	}
	return n, i, nil
}
