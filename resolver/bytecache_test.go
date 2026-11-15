package resolver

import (
	"fmt"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextdns/nextdns/resolver/query"
	"golang.org/x/net/dns/dnsmessage"
)

// testCacheValue returns a cache value for k holding a valid DNS response of
// at least size bytes, built the same way the resolvers build cache entries.
func testCacheValue(t testing.TB, k cacheKey, size int) *cacheValue {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true})
	if err := b.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := b.Question(dnsmessage.Question{
		Name:  dnsmessage.MustNewName(k.qname),
		Type:  dnsmessage.Type(k.qtype),
		Class: dnsmessage.Class(k.qclass),
	}); err != nil {
		t.Fatal(err)
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(msg) < size {
		// Trailing bytes are ignored by the question parser.
		msg = append(msg, make([]byte, size-len(msg))...)
	}
	return &cacheValue{time: time.Now(), msg: msg, ctx: k.ctx}
}

// queueSets runs the Sets in fn so that they are all queued before Ristretto's
// processing goroutine handles any of them: with a single P, the goroutine is
// only made runnable by the channel sends and runs once the caller blocks in
// Wait.
func queueSets(t *testing.T, bc *ByteCache, fn func()) {
	t.Helper()
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	fn()
	bc.c.Wait()
}

func testKey(i int) cacheKey {
	return cacheKey{"https://dns.example/abc", query.ClassINET, query.TypeA, fmt.Sprintf("host%d.example.", i)}
}

func newTestByteCache(t testing.TB, maxCost uint64) *ByteCache {
	t.Helper()
	bc, err := NewByteCache(maxCost, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bc.c.Close)
	return bc
}

// storedKeys returns the sorted keys among ks that Get finds in the cache.
func storedKeys(bc *ByteCache, ks []cacheKey) []string {
	out := []string{}
	for _, k := range ks {
		if _, ok := bc.Get(k.Hash()); ok {
			out = append(out, k.String())
		}
	}
	slices.Sort(out)
	return out
}

func assertKeys(t *testing.T, bc *ByteCache, want []string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	got := bc.Keys()
	if slices.Equal(got, want) {
		return
	}
	if len(got) <= 4 && len(want) <= 4 {
		t.Fatalf("Keys() = %q, want %q", got, want)
	}
	var missing, extra []string
	for _, k := range want {
		if !slices.Contains(got, k) {
			missing = append(missing, k)
		}
	}
	for _, k := range got {
		if !slices.Contains(want, k) {
			extra = append(extra, k)
		}
	}
	t.Fatalf("Keys() has %d entries, want %d: %d missing (e.g. %q), %d extra (e.g. %q)",
		len(got), len(want), len(missing), missing[:min(len(missing), 2)], len(extra), extra[:min(len(extra), 2)])
}

// TestByteCacheKeysOversizedThenSmaller is the sequence from the #1165 review:
// a write larger than the whole cache followed by a smaller write for the same
// key. Ristretto rejects the first and admits the second.
func TestByteCacheKeysOversizedThenSmaller(t *testing.T) {
	bc := newTestByteCache(t, 4096)
	k := testKey(1)
	big, small := testCacheValue(t, k, 8192), testCacheValue(t, k, 100)
	queueSets(t, bc, func() {
		bc.Set(k.Hash(), big)
		bc.Set(k.Hash(), small)
	})

	if _, ok := bc.Get(k.Hash()); !ok {
		t.Fatal("Get() miss, want hit")
	}
	assertKeys(t, bc, []string{k.String()})
}

func TestByteCacheKeysRejected(t *testing.T) {
	bc := newTestByteCache(t, 4096)
	k := testKey(1)
	bc.Set(k.Hash(), testCacheValue(t, k, 8192))
	bc.c.Wait()

	if _, ok := bc.Get(k.Hash()); ok {
		t.Fatal("Get() hit, want miss")
	}
	assertKeys(t, bc, nil)
}

// TestByteCacheKeysQueuedOverwrite queues two writes for the same new key.
// Ristretto admits the first and rejects the second as a duplicate, keeping
// the first value.
func TestByteCacheKeysQueuedOverwrite(t *testing.T) {
	bc := newTestByteCache(t, 1<<20)
	k := testKey(1)
	v1, v2 := testCacheValue(t, k, 100), testCacheValue(t, k, 200)
	queueSets(t, bc, func() {
		bc.Set(k.Hash(), v1)
		bc.Set(k.Hash(), v2)
	})

	if _, ok := bc.Get(k.Hash()); !ok {
		t.Fatal("Get() miss, want hit")
	}
	assertKeys(t, bc, []string{k.String()})
}

func TestByteCacheKeysOverwrite(t *testing.T) {
	bc := newTestByteCache(t, 1<<20)
	k := testKey(1)
	bc.Set(k.Hash(), testCacheValue(t, k, 100))
	bc.c.Wait()
	v2 := testCacheValue(t, k, 200)
	bc.Set(k.Hash(), v2)
	bc.c.Wait()

	if v, ok := bc.Get(k.Hash()); !ok || v != v2 {
		t.Fatalf("Get() = %p, %v, want %p, true", v, ok, v2)
	}
	assertKeys(t, bc, []string{k.String()})
}

func TestByteCacheKeysEvicted(t *testing.T) {
	bc := newTestByteCache(t, 16<<10)
	var ks []cacheKey
	for i := range 200 {
		k := testKey(i)
		ks = append(ks, k)
		bc.Set(k.Hash(), testCacheValue(t, k, 512))
		bc.c.Wait()
	}
	want := storedKeys(bc, ks)
	if len(want) == 0 || len(want) == len(ks) {
		t.Fatalf("%d of %d entries stored, want some evicted", len(want), len(ks))
	}
	assertKeys(t, bc, want)
}

// TestByteCacheKeysExpired covers TTL expiry. The resolvers do not set a
// Ristretto TTL today; this guards the listing if they ever do.
func TestByteCacheKeysExpired(t *testing.T) {
	bc := newTestByteCache(t, 1<<20)
	k1, k2 := testKey(1), testKey(2)
	v := testCacheValue(t, k1, 100)
	bc.c.SetWithTTL(k1.Hash(), v, int64(len(v.msg)), 50*time.Millisecond)
	bc.Set(k2.Hash(), testCacheValue(t, k2, 100))
	bc.c.Wait()
	assertKeys(t, bc, []string{k1.String(), k2.String()})

	time.Sleep(100 * time.Millisecond)
	if _, ok := bc.Get(k1.Hash()); ok {
		t.Fatal("Get() hit for expired entry, want miss")
	}
	assertKeys(t, bc, []string{k2.String()})
}

func TestByteCacheKeysClear(t *testing.T) {
	bc := newTestByteCache(t, 1<<20)
	k := testKey(1)
	bc.Set(k.Hash(), testCacheValue(t, k, 100))
	bc.c.Wait()
	bc.c.Clear()
	assertKeys(t, bc, nil)

	bc.Set(k.Hash(), testCacheValue(t, k, 100))
	bc.c.Wait()
	assertKeys(t, bc, []string{k.String()})
}

func TestByteCacheKeysClosedAndNil(t *testing.T) {
	bc := newTestByteCache(t, 1<<20)
	k := testKey(1)
	bc.Set(k.Hash(), testCacheValue(t, k, 100))
	bc.c.Wait()
	bc.c.Close()
	assertKeys(t, bc, nil)

	var nilCache *ByteCache
	if got := nilCache.Keys(); got == nil || len(got) != 0 {
		t.Fatalf("nil Keys() = %#v, want empty non-nil slice", got)
	}
}

func TestByteCacheKeysFormat(t *testing.T) {
	bc := newTestByteCache(t, 1<<20)
	dns53 := cacheKey{"", query.ClassINET, query.TypeAAAA, "example.com."}
	doh := cacheKey{"https://dns.nextdns.io/abc", query.ClassINET, query.TypeA, "example.com."}
	for _, k := range []cacheKey{dns53, doh} {
		bc.Set(k.Hash(), testCacheValue(t, k, 0))
	}
	bc.Set(99, &cacheValue{msg: []byte{1, 2, 3}}) // unparsable, not listed
	bc.c.Wait()
	assertKeys(t, bc, []string{" INET AAAA example.com.", "https://dns.nextdns.io/abc INET A example.com."})
}

// TestByteCacheKeysConcurrent lists keys while writers and readers run, then
// checks the listing matches the store once the writers have stopped.
func TestByteCacheKeysConcurrent(t *testing.T) {
	bc := newTestByteCache(t, 64<<10)
	const n = 500
	ks := make([]cacheKey, n)
	vs := make([]*cacheValue, n)
	big := make([]*cacheValue, n)
	for i := range n {
		ks[i] = testKey(i)
		vs[i] = testCacheValue(t, ks[i], 100+i%400)
		big[i] = testCacheValue(t, ks[i], 128<<10)
	}
	var stop atomic.Bool
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Go(func() {
			for i := w; !stop.Load(); i++ {
				j := i % n
				if i%7 == 0 {
					bc.Set(ks[j].Hash(), big[j])
				}
				bc.Set(ks[j].Hash(), vs[j])
				bc.Get(ks[(i*31)%n].Hash())
			}
		})
	}
	wg.Go(func() {
		for !stop.Load() {
			for _, k := range bc.Keys() {
				if k == "" {
					t.Error("empty key listed")
					return
				}
			}
		}
	})
	time.Sleep(200 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
	bc.c.Wait()
	assertKeys(t, bc, storedKeys(bc, ks))
}
