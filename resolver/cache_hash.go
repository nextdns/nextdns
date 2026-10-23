package resolver

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"sync/atomic"
	"unsafe"

	"github.com/cespare/xxhash/v2"
	"github.com/nextdns/nextdns/resolver/query"
)

// CacheHash identifies the hash function used to derive cache keys.
type CacheHash uint32

const (
	// CacheHashXXHash uses 64-bit xxHash (default).
	CacheHashXXHash CacheHash = iota
	// CacheHashCRC32 uses CRC-32C (Castagnoli) from the standard library, which
	// is hardware accelerated on many arm64 and amd64 CPUs. The resulting
	// 32-bit value is zero-extended; collisions are harmless as cached entries
	// are validated against the question on lookup.
	CacheHashCRC32
)

// CacheHashNames lists the accepted names, in CacheHash order.
var CacheHashNames = []string{"xxhash", "crc32"}

var (
	cacheHashAlgo atomic.Uint32
	crc32cTable   = crc32.MakeTable(crc32.Castagnoli)
)

func (h CacheHash) String() string {
	if int(h) < len(CacheHashNames) {
		return CacheHashNames[h]
	}
	return fmt.Sprintf("CacheHash(%d)", uint32(h))
}

// ParseCacheHash parses a cache hash name. An empty name selects the default.
func ParseCacheHash(name string) (CacheHash, error) {
	if name == "" {
		return CacheHashXXHash, nil
	}
	for i, n := range CacheHashNames {
		if n == name {
			return CacheHash(i), nil
		}
	}
	return 0, fmt.Errorf("%q: unknown cache hash, must be one of %v", name, CacheHashNames)
}

// SetCacheHash selects the hash function used for cache keys. It must be
// called before the cache is populated, as changing it orphans existing
// entries.
func SetCacheHash(h CacheHash) {
	cacheHashAlgo.Store(uint32(h))
}

// hashBufSize is the size of the stack buffer used to hash a key in one call
// with xxhash. It fits a DoH context URL plus a typical query name; longer
// keys are hashed incrementally.
const hashBufSize = 256

func (k cacheKey) hashWith(h CacheHash) uint64 {
	if h == CacheHashCRC32 {
		// Seed with class and type, then chain ctx and qname. The strings are
		// not copied: crc32.Update escapes its slice argument, so building a
		// buffer would cost a heap allocation.
		c := uint32(k.qclass)<<16 | uint32(k.qtype)
		c = crc32.Update(c, crc32cTable, unsafe.Slice(unsafe.StringData(k.ctx), len(k.ctx)))
		c = crc32.Update(c, crc32cTable, unsafe.Slice(unsafe.StringData(k.qname), len(k.qname)))
		return uint64(c)
	}
	if n := len(k.ctx) + 4 + len(k.qname); n <= hashBufSize {
		var buf [hashBufSize]byte
		i := copy(buf[:], k.ctx)
		binary.BigEndian.PutUint16(buf[i:], uint16(k.qclass))
		binary.BigEndian.PutUint16(buf[i+2:], uint16(k.qtype))
		copy(buf[i+4:], k.qname)
		return xxhash.Sum64(buf[:n])
	}
	var b [4]byte
	binary.BigEndian.PutUint16(b[0:2], uint16(k.qclass))
	binary.BigEndian.PutUint16(b[2:4], uint16(k.qtype))
	d := xxhash.New()
	_, _ = d.Write(unsafe.Slice(unsafe.StringData(k.ctx), len(k.ctx)))
	_, _ = d.Write(b[:])
	_, _ = d.Write(unsafe.Slice(unsafe.StringData(k.qname), len(k.qname)))
	return d.Sum64()
}

// HashKey computes the cache key hash of a question with the given algorithm.
// It is exported for benchmarking.
func HashKey(h CacheHash, ctx string, qclass query.Class, qtype query.Type, qname string) uint64 {
	return cacheKey{ctx, qclass, qtype, qname}.hashWith(h)
}
