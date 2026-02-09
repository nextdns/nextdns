package resolver

import (
	"strconv"
	"testing"

	"github.com/dgraph-io/ristretto/v2"
)

func newBenchCache(b *testing.B, metrics bool) *ristretto.Cache[uint64, []byte] {
	b.Helper()
	c, err := ristretto.NewCache(&ristretto.Config[uint64, []byte]{
		NumCounters: 1_000_00,  // keep small-ish for benchmark stability
		MaxCost:     64 << 20,  // 64MiB
		BufferItems: 64,
		Metrics:     metrics,
	})
	if err != nil {
		b.Fatalf("NewCache: %v", err)
	}
	return c
}

func BenchmarkRistretto_GetHit_MetricsOff(b *testing.B) { benchRistrettoGetHit(b, false) }
func BenchmarkRistretto_GetHit_MetricsOn(b *testing.B)  { benchRistrettoGetHit(b, true) }

func benchRistrettoGetHit(b *testing.B, metrics bool) {
	c := newBenchCache(b, metrics)
	defer c.Close()

	// Pre-populate and ensure visibility.
	const entries = 4096
	val := make([]byte, 512)
	for i := 0; i < entries; i++ {
		c.Set(uint64(i), val, int64(len(val)))
	}
	c.Wait()

	b.ReportAllocs()
	b.ResetTimer()

	var sink []byte
	for i := 0; i < b.N; i++ {
		v, _ := c.Get(uint64(i & (entries - 1)))
		sink = v
	}
	_ = sink
}

func BenchmarkRistretto_GetMiss_MetricsOff(b *testing.B) { benchRistrettoGetMiss(b, false) }
func BenchmarkRistretto_GetMiss_MetricsOn(b *testing.B)  { benchRistrettoGetMiss(b, true) }

func benchRistrettoGetMiss(b *testing.B, metrics bool) {
	c := newBenchCache(b, metrics)
	defer c.Close()

	b.ReportAllocs()
	b.ResetTimer()

	var sink bool
	for i := 0; i < b.N; i++ {
		_, ok := c.Get(uint64(i))
		sink = ok
	}
	_ = sink
}

func BenchmarkRistretto_Set_MetricsOff(b *testing.B) { benchRistrettoSet(b, false) }
func BenchmarkRistretto_Set_MetricsOn(b *testing.B)  { benchRistrettoSet(b, true) }

func benchRistrettoSet(b *testing.B, metrics bool) {
	c := newBenchCache(b, metrics)
	defer c.Close()

	// Value size approximates a cached DNS response payload.
	val := make([]byte, 900)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Use a changing key to avoid just updating one entry.
		c.Set(uint64(i), val, int64(len(val)))
	}
}

func BenchmarkRistretto_GetHit_Parallel_MetricsOff(b *testing.B) {
	benchRistrettoGetHitParallel(b, false)
}
func BenchmarkRistretto_GetHit_Parallel_MetricsOn(b *testing.B) {
	benchRistrettoGetHitParallel(b, true)
}

func benchRistrettoGetHitParallel(b *testing.B, metrics bool) {
	c := newBenchCache(b, metrics)
	defer c.Close()

	const entries = 1 << 16
	val := make([]byte, 256)
	for i := 0; i < entries; i++ {
		c.Set(uint64(i), val, int64(len(val)))
	}
	c.Wait()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		// Give each worker a distinct offset so they don't all hammer the same key.
		// strconv is a cheap way to get some per-goroutine entropy here.
		var off uint64
		if s := strconv.Itoa(int(b.N)); len(s) > 0 {
			off = uint64(s[0])
		}
		var i uint64
		for pb.Next() {
			_, _ = c.Get((i + off) & (entries - 1))
			i++
		}
	})
}

