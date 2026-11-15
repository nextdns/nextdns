package resolver

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newKeysBenchCache(b *testing.B, entries int) (*ByteCache, []cacheKey, []*cacheValue) {
	b.Helper()
	bc, err := NewByteCache(256<<20, false)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(bc.c.Close)
	ks := make([]cacheKey, entries)
	vs := make([]*cacheValue, entries)
	for i := range entries {
		ks[i] = testKey(i)
		vs[i] = testCacheValue(b, ks[i], 512)
		bc.Set(ks[i].Hash(), vs[i])
		if i%1000 == 0 {
			bc.c.Wait()
		}
	}
	bc.c.Wait()
	return bc, ks, vs
}

func BenchmarkByteCache_Keys_64k(b *testing.B) {
	bc, _, _ := newKeysBenchCache(b, 1<<16)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = bc.Keys()
	}
}

// BenchmarkByteCache_SetGet_Parallel measures the query path (Get then Set)
// under parallel load, with and without cache-keys running in a loop. It also
// reports the p99 and max latency of the Set calls, which are the writes a
// listing could block.
func BenchmarkByteCache_SetGet_Parallel(b *testing.B) {
	for _, withKeys := range []bool{false, true} {
		name := "NoKeys"
		if withKeys {
			name = "KeysLoop"
		}
		b.Run(name, func(b *testing.B) {
			const entries = 1 << 16
			bc, ks, vs := newKeysBenchCache(b, entries)
			var stop atomic.Bool
			var wg sync.WaitGroup
			var listings atomic.Int64
			if withKeys {
				wg.Go(func() {
					for !stop.Load() {
						_ = bc.Keys()
						listings.Add(1)
					}
				})
			}
			var next atomic.Uint64
			var mu sync.Mutex
			var lat []time.Duration
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := next.Add(7919)
				local := make([]time.Duration, 0, 1<<16)
				for pb.Next() {
					j := i % entries
					if _, ok := bc.Get(ks[j].Hash()); !ok || i%8 == 0 {
						start := time.Now()
						bc.Set(ks[j].Hash(), vs[j])
						local = append(local, time.Since(start))
					}
					i++
				}
				mu.Lock()
				lat = append(lat, local...)
				mu.Unlock()
			})
			b.StopTimer()
			if len(lat) > 0 {
				slices.Sort(lat)
				b.ReportMetric(float64(lat[len(lat)*99/100].Nanoseconds()), "p99-set-ns")
				b.ReportMetric(float64(lat[len(lat)-1].Nanoseconds()), "max-set-ns")
			}
			stop.Store(true)
			wg.Wait()
			if withKeys {
				b.ReportMetric(float64(listings.Load()), "listings")
			}
		})
	}
}
