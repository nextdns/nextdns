package resolver

import (
	"errors"
	"math"

	"github.com/dgraph-io/ristretto/v2"
)

// ByteCache is a byte-limited cache implementation for DNS responses.
// It is backed by Ristretto and uses cost in bytes for eviction decisions.
type ByteCache struct {
	c *ristretto.Cache[uint64, *cacheValue]
}

// NewByteCache creates a new byte-limited cache with maxCost expressed in bytes.
func NewByteCache(maxCost uint64) (*ByteCache, error) {
	if maxCost == 0 {
		return nil, errors.New("maxCost must be > 0")
	}

	mc := int64(maxCost)
	if maxCost > uint64(math.MaxInt64) {
		mc = math.MaxInt64
	}

	// NumCounters should be ~10x the number of expected items. We don't know the
	// average entry size, so approximate 1KiB per entry and clamp.
	estEntries := mc / 1024
	if estEntries < 1024 {
		estEntries = 1024
	}
	numCounters := estEntries * 10
	if numCounters < 1<<12 {
		numCounters = 1 << 12
	}
	if numCounters > 100_000_000 {
		numCounters = 100_000_000
	}

	rc, err := ristretto.NewCache(&ristretto.Config[uint64, *cacheValue]{
		NumCounters: numCounters,
		MaxCost:     mc,
		BufferItems: 64,
		Metrics:     true,
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

func (bc *ByteCache) Set(key uint64, value *cacheValue, cost int64) {
	if bc == nil || bc.c == nil || value == nil {
		return
	}
	if cost <= 0 {
		cost = 1
	}
	// Ristretto's Set is async and may be dropped under contention.
	_ = bc.c.Set(key, value, cost)
}

// Metrics returns Ristretto metrics (may be nil if metrics are disabled).
func (bc *ByteCache) Metrics() *ristretto.Metrics {
	if bc == nil || bc.c == nil {
		return nil
	}
	return bc.c.Metrics
}

