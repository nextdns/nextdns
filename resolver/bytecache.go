package resolver

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/dgraph-io/ristretto/v2"
)

// ByteCache is a byte-limited cache implementation for DNS responses.
// It is backed by Ristretto and uses cost in bytes for eviction decisions.
type ByteCache struct {
	c      *ristretto.Cache[uint64, *cacheValue]
	mu     sync.Mutex
	labels map[uint64]string
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

	bc := &ByteCache{labels: map[uint64]string{}}
	rc, err := ristretto.NewCache(&ristretto.Config[uint64, *cacheValue]{
		NumCounters: numCounters,
		MaxCost:     mc,
		BufferItems: 64,
		Metrics:     metrics,
		OnEvict: func(item *ristretto.Item[*cacheValue]) {
			if item != nil {
				bc.removeLabel(item.Key)
			}
		},
		OnReject: func(item *ristretto.Item[*cacheValue]) {
			if item != nil {
				bc.removeLabel(item.Key)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	bc.c = rc
	return bc, nil
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
	bc.storeLabel(key, value.label)
	// Ristretto's Set is async and may be dropped under contention.
	if !bc.c.Set(key, value, cost) {
		bc.removeLabel(key)
	}
}

// Keys returns the human-readable labels of entries currently tracked in the
// cache. Ristretto does not enumerate its store, so labels are kept alongside
// Set/evict/reject. The list may briefly include keys that are still pending
// admission.
func (bc *ByteCache) Keys() []string {
	if bc == nil {
		return nil
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	out := make([]string, 0, len(bc.labels))
	for _, label := range bc.labels {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

func (bc *ByteCache) storeLabel(key uint64, label string) {
	if label == "" {
		label = fmt.Sprintf("%016x", key)
	}
	bc.mu.Lock()
	bc.labels[key] = label
	bc.mu.Unlock()
}

func (bc *ByteCache) removeLabel(key uint64) {
	bc.mu.Lock()
	delete(bc.labels, key)
	bc.mu.Unlock()
}

// Metrics returns Ristretto metrics (may be nil if metrics are disabled).
func (bc *ByteCache) Metrics() *ristretto.Metrics {
	if bc == nil || bc.c == nil {
		return nil
	}
	return bc.c.Metrics
}
