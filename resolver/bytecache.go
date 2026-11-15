package resolver

import (
	"errors"
	"math"
	"slices"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/nextdns/nextdns/resolver/query"
	"golang.org/x/net/dns/dnsmessage"
)

// ByteCache is a byte-limited cache implementation for DNS responses.
// It is backed by Ristretto and uses cost in bytes for eviction decisions.
type ByteCache struct {
	c *ristretto.Cache[uint64, *cacheValue]
}

// NewByteCache creates a new byte-limited cache with maxCost expressed in bytes.
// If metrics is false, cache metrics collection is disabled.
func NewByteCache(maxCost uint64, metrics bool) (*ByteCache, error) {
	if maxCost == 0 {
		return nil, errors.New("maxCost must be > 0")
	}

	mc := int64(maxCost)
	if maxCost > uint64(math.MaxInt64) {
		mc = math.MaxInt64
	}

	// NumCounters should be ~10x the number of expected items. We don't know the
	// average entry size, so approximate 1KiB per entry and clamp.
	estEntries := max(mc/1024, 1024)
	numCounters := max(estEntries*10, 1<<12)
	if numCounters > 100_000_000 {
		numCounters = 100_000_000
	}

	rc, err := ristretto.NewCache(&ristretto.Config[uint64, *cacheValue]{
		NumCounters: numCounters,
		MaxCost:     mc,
		BufferItems: 64,
		Metrics:     metrics,
	})
	if err != nil {
		return nil, err
	}
	return &ByteCache{c: rc}, nil
}

func (bc *ByteCache) Get(key uint64) (value *cacheValue, ok bool) {
	if bc == nil || bc.c == nil {
		return nil, false
	}
	return bc.c.Get(key)
}

func (bc *ByteCache) Set(key uint64, value *cacheValue) {
	if bc == nil || bc.c == nil || value == nil {
		return
	}
	cost := int64(len(value.msg))
	if cost <= 0 {
		cost = 1
	}
	// Ristretto's Set is async and may be dropped under contention.
	_ = bc.c.Set(key, value, cost)
}

// Keys returns the sorted keys of the entries currently stored in the cache,
// formatted like cacheKey.String. It reads Ristretto's store directly, so
// entries that were rejected, evicted, replaced, expired or cleared are never
// listed.
// Only pointers are collected while a store shard is read-locked; parsing and
// sorting happen after the shard locks are released.
func (bc *ByteCache) Keys() []string {
	keys := []string{}
	if bc == nil || bc.c == nil {
		return keys
	}
	var values []*cacheValue
	bc.c.IterValues(func(v *cacheValue) bool {
		values = append(values, v)
		return false
	})
	for _, v := range values {
		if k, ok := v.key(); ok {
			keys = append(keys, k.String())
		}
	}
	slices.Sort(keys)
	return keys
}

// key rebuilds the cache key of v from its context and the question of the
// stored response.
func (v *cacheValue) key() (cacheKey, bool) {
	if v == nil {
		return cacheKey{}, false
	}
	var p dnsmessage.Parser
	if _, err := p.Start(v.msg); err != nil {
		return cacheKey{}, false
	}
	q, err := p.Question()
	if err != nil {
		return cacheKey{}, false
	}
	return cacheKey{v.ctx, query.Class(q.Class), query.Type(q.Type), q.Name.String()}, true
}

// Metrics returns Ristretto metrics (may be nil if metrics are disabled).
func (bc *ByteCache) Metrics() *ristretto.Metrics {
	if bc == nil || bc.c == nil {
		return nil
	}
	return bc.c.Metrics
}
