//go:build darwin || windows || linux
// +build darwin windows linux

package host

import (
	"sync/atomic"
	"time"
)

type cachedStringValue struct {
	value     string
	refreshAt int64
}

type refreshingStringCache struct {
	ttl      time.Duration
	cached   atomic.Pointer[cachedStringValue]
	updating atomic.Bool
}

func newRefreshingStringCache(ttl time.Duration) refreshingStringCache {
	return refreshingStringCache{ttl: ttl}
}

func (c *refreshingStringCache) Get(load func() string) string {
	now := time.Now().UnixNano()
	v := c.cached.Load()
	if v != nil && now < v.refreshAt {
		return v.value
	}

	// Ensure only one caller refreshes the cache at a time. Others keep using
	// the previous cached value to avoid blocking.
	if c.updating.CompareAndSwap(false, true) {
		defer c.updating.Store(false)
		s := load()
		c.cached.Store(&cachedStringValue{
			value:     s,
			refreshAt: time.Now().Add(c.ttl).UnixNano(),
		})
		return s
	}

	if v != nil {
		return v.value
	}
	return ""
}
