package resolver

import (
	"errors"
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/nextdns/nextdns/internal/dnsmessage"
	"github.com/nextdns/nextdns/resolver/query"
)

// CacheEntry represents a single cached DNS response for external inspection.
type CacheEntry struct {
	Key       string        `json:"key"`
	Domain    string        `json:"domain"`
	Type      string        `json:"type"`
	Class     string        `json:"class"`
	TTL       uint32        `json:"ttl"`
	Age       float64       `json:"age"`
	Size      int           `json:"size"`
	Transport string        `json:"transport"`
	Answers   []AnswerEntry `json:"answers"`
}

// AnswerEntry represents a single resource record in a cached DNS response.
type AnswerEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
	TTL  uint32 `json:"ttl"`
	Data string `json:"data"`
}

// ByteCache is a byte-limited cache implementation for DNS responses.
// It is backed by Ristretto and uses cost in bytes for eviction decisions.
type ByteCache struct {
	c    *ristretto.Cache[uint64, *cacheValue]
	keys sync.Map // uint64 -> string (cacheKey.String())
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

	bc := &ByteCache{}
	rc, err := ristretto.NewCache(&ristretto.Config[uint64, *cacheValue]{
		NumCounters: numCounters,
		MaxCost:     mc,
		BufferItems: 64,
		Metrics:     metrics,
		OnEvict: func(item *ristretto.Item[*cacheValue]) {
			bc.keys.Delete(item.Key)
		},
		OnReject: func(item *ristretto.Item[*cacheValue]) {
			bc.keys.Delete(item.Key)
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
	// Build key string from the DNS response question section.
	bc.keys.Store(key, parseCacheKeyString("", value.msg))
	// Ristretto's Set is async and may be dropped under contention.
	_ = bc.c.Set(key, value, cost)
}

// SetTracked stores a cache entry and records the full key string including
// the context (e.g. DoH URL) for later enumeration via Dump().
func (bc *ByteCache) SetTracked(key uint64, value *cacheValue, ctx string) {
	if bc == nil || bc.c == nil || value == nil {
		return
	}
	cost := int64(len(value.msg))
	if cost <= 0 {
		cost = 1
	}
	bc.keys.Store(key, parseCacheKeyString(ctx, value.msg))
	_ = bc.c.Set(key, value, cost)
}

// Metrics returns Ristretto metrics (may be nil if metrics are disabled).
func (bc *ByteCache) Metrics() *ristretto.Metrics {
	if bc == nil || bc.c == nil {
		return nil
	}
	return bc.c.Metrics
}

// Dump returns all currently cached entries with full response data.
func (bc *ByteCache) Dump() []CacheEntry {
	if bc == nil || bc.c == nil {
		return nil
	}
	now := time.Now()
	var entries []CacheEntry
	bc.keys.Range(func(k, v any) bool {
		key := k.(uint64)
		keyStr, _ := v.(string)
		val, ok := bc.c.Get(key)
		if !ok || val == nil {
			bc.keys.Delete(key)
			return true
		}
		entry := buildCacheEntry(val, keyStr, now)
		if entry != nil {
			entries = append(entries, *entry)
		}
		return true
	})
	return entries
}

// parseCacheKeyString extracts domain/type/class from a DNS message and
// formats a key string matching the cacheKey.String() format.
func parseCacheKeyString(ctx string, msg []byte) string {
	if len(msg) < 12 {
		return ctx
	}
	var p dnsmessage.Parser
	if _, err := p.Start(msg); err != nil {
		return ctx
	}
	q, err := p.Question()
	if err != nil {
		return ctx
	}
	return fmt.Sprintf("%s %s %s %s", ctx, query.Class(q.Class), query.Type(q.Type), q.Name.String())
}

// buildCacheEntry parses a cacheValue into a CacheEntry with answer records.
func buildCacheEntry(v *cacheValue, keyStr string, now time.Time) *CacheEntry {
	if len(v.msg) < 12 {
		return nil
	}
	var p dnsmessage.Parser
	if _, err := p.Start(v.msg); err != nil {
		return nil
	}
	q, err := p.Question()
	if err != nil {
		return nil
	}

	age := now.Sub(v.time)
	ageSec := uint32(age / time.Second)

	// Compute remaining min TTL on a copy (updateTTL mutates in place).
	msgCopy := append([]byte(nil), v.msg...)
	minTTL := updateTTL(msgCopy, ageSec, 0, 0)

	// Parse answer records from the age-adjusted copy.
	answers := parseAnswers(msgCopy)

	return &CacheEntry{
		Key:       keyStr,
		Domain:    q.Name.String(),
		Type:      query.Type(q.Type).String(),
		Class:     query.Class(q.Class).String(),
		TTL:       minTTL,
		Age:       age.Seconds(),
		Size:      len(v.msg),
		Transport: v.trans,
		Answers:   answers,
	}
}

// parseAnswers extracts answer resource records from a DNS message.
func parseAnswers(msg []byte) []AnswerEntry {
	var p dnsmessage.Parser
	if _, err := p.Start(msg); err != nil {
		return nil
	}
	if err := p.SkipAllQuestions(); err != nil {
		return nil
	}
	var answers []AnswerEntry
	for {
		hdr, err := p.AnswerHeader()
		if err != nil {
			break
		}
		data := formatResourceData(&p, hdr.Type)
		answers = append(answers, AnswerEntry{
			Name: hdr.Name.String(),
			Type: query.Type(hdr.Type).String(),
			TTL:  hdr.TTL,
			Data: data,
		})
	}
	return answers
}

// formatResourceData reads the resource body and returns a human-readable string.
func formatResourceData(p *dnsmessage.Parser, typ dnsmessage.Type) string {
	switch typ {
	case dnsmessage.TypeA:
		r, err := p.AResource()
		if err != nil {
			return ""
		}
		return net.IP(r.A[:]).String()
	case dnsmessage.TypeAAAA:
		r, err := p.AAAAResource()
		if err != nil {
			return ""
		}
		return net.IP(r.AAAA[:]).String()
	case dnsmessage.TypeCNAME:
		r, err := p.CNAMEResource()
		if err != nil {
			return ""
		}
		return r.CNAME.String()
	case dnsmessage.TypeMX:
		r, err := p.MXResource()
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%d %s", r.Pref, r.MX.String())
	case dnsmessage.TypeNS:
		r, err := p.NSResource()
		if err != nil {
			return ""
		}
		return r.NS.String()
	case dnsmessage.TypePTR:
		r, err := p.PTRResource()
		if err != nil {
			return ""
		}
		return r.PTR.String()
	case dnsmessage.TypeSOA:
		r, err := p.SOAResource()
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%s %s %d %d %d %d %d",
			r.NS.String(), r.MBox.String(),
			r.Serial, r.Refresh, r.Retry, r.Expire, r.MinTTL)
	case dnsmessage.TypeTXT:
		r, err := p.TXTResource()
		if err != nil {
			return ""
		}
		if len(r.TXT) == 1 {
			return r.TXT[0]
		}
		return fmt.Sprintf("%v", r.TXT)
	case dnsmessage.TypeSRV:
		r, err := p.SRVResource()
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%d %d %d %s", r.Priority, r.Weight, r.Port, r.Target.String())
	default:
		_ = p.SkipAnswer()
		return fmt.Sprintf("(type %d)", typ)
	}
}

