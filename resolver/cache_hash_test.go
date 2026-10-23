package resolver

import (
	"testing"

	"github.com/nextdns/nextdns/resolver/query"
)

func TestParseCacheHash(t *testing.T) {
	for name, want := range map[string]CacheHash{"": CacheHashXXHash, "xxhash": CacheHashXXHash, "crc32": CacheHashCRC32} {
		got, err := ParseCacheHash(name)
		if err != nil || got != want {
			t.Errorf("ParseCacheHash(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
	if _, err := ParseCacheHash("md5"); err == nil {
		t.Error("expected error for unknown hash")
	}
}

func TestCacheKeyHashAlgos(t *testing.T) {
	for _, h := range []CacheHash{CacheHashXXHash, CacheHashCRC32} {
		a := HashKey(h, "ctx", query.ClassINET, query.TypeA, "example.com.")
		if a != HashKey(h, "ctx", query.ClassINET, query.TypeA, "example.com.") {
			t.Errorf("%v: not deterministic", h)
		}
		if a == HashKey(h, "ctx", query.ClassINET, query.TypeAAAA, "example.com.") {
			t.Errorf("%v: qtype ignored", h)
		}
		if a == HashKey(h, "other", query.ClassINET, query.TypeA, "example.com.") {
			t.Errorf("%v: ctx ignored", h)
		}
	}
}

func BenchmarkCacheKeyHash(b *testing.B) {
	for _, h := range []CacheHash{CacheHashXXHash, CacheHashCRC32} {
		b.Run(h.String(), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				HashKey(h, "https://dns.nextdns.io/abcdef", query.ClassINET, query.TypeA, "www.example.com.")
			}
		})
	}
}
