package resolver

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/nextdns/nextdns/resolver/query"
)

func TestCacheKeyHashCompatibility(t *testing.T) {
	contexts := []string{"", "https://dns.nextdns.io/abcdef", strings.Repeat("context", 40)}
	questions := []struct {
		class query.Class
		qtype query.Type
	}{
		{query.ClassINET, query.TypeA},
		{query.ClassINET, query.TypeAAAA},
		{query.ClassCHAOS, query.TypeTXT},
		{0, 0},
		{0xffff, 0xffff},
	}
	// Sweep every length through the stack-buffer boundary, including empty
	// strings and binary bytes. The hash must preserve the original encoding.
	var name [301]byte
	for i := range name {
		name[i] = byte(i)
	}
	for _, ctx := range contexts {
		for _, q := range questions {
			for n := 0; n <= len(name); n++ {
				k := cacheKey{ctx, q.class, q.qtype, string(name[:n])}
				if got, want := k.Hash(), incrementalCacheKeyHash(k); got != want {
					t.Fatalf("context length %d, name length %d, class %d, type %d: got %x, want %x",
						len(ctx), n, q.class, q.qtype, got, want)
				}
			}
		}
	}
}

// incrementalCacheKeyHash preserves the original streaming implementation as
// an independent reference for compatibility tests.
func incrementalCacheKeyHash(k cacheKey) uint64 {
	d := xxhash.New()
	_, _ = d.WriteString(k.ctx)
	var b [4]byte
	binary.BigEndian.PutUint16(b[0:2], uint16(k.qclass))
	binary.BigEndian.PutUint16(b[2:4], uint16(k.qtype))
	_, _ = d.Write(b[:])
	_, _ = d.WriteString(k.qname)
	return d.Sum64()
}

// Keep the corpus from PR #1134 so measurements remain comparable, but exercise
// every name with both contexts and query types instead of coupling them to the
// parity of a generated index. These are synthetic workloads, not traffic weights.
var cacheHashBenchNames = []string{
	"t.co.", "x.com.", "fb.com.", "bit.ly.", "ft.com.",
	"google.com.", "apple.com.", "amazon.com.", "netflix.com.", "github.com.",
	"www.google.com.", "www.youtube.com.", "api.github.com.", "mail.yahoo.com.",
	"login.microsoftonline.com.", "play.googleapis.com.", "cdn.jsdelivr.net.",
	"graph.facebook.com.", "clients4.google.com.", "ocsp.digicert.com.",
	"time.cloudflare.com.", "update.googleapis.com.", "static.xx.fbcdn.net.",
	"d1234abcd5efgh.cloudfront.net.", "e1234.dscb.akamaiedge.net.",
	"safebrowsing.googleapis.com.", "gateway.icloud.com.akadns.net.",
	"android.clients.google.com.", "settings-win.data.microsoft.com.",
	"s3-us-west-2.amazonaws.com.", "ec2-54-12-34-56.compute-1.amazonaws.com.",
	"prod-eu-west-1-lb-1234567890.elb.eu-west-1.amazonaws.com.",
	"a248.e.akamai.net.", "scontent-lhr8-1.xx.fbcdn.net.",
	"pagead2.googlesyndication.com.", "securepubads.g.doubleclick.net.",
	"device-metrics-us-2.amazon.com.", "mobile.events.data.microsoft.com.",
	"v10.events.data.microsoft.com.", "self.events.data.microsoft.com.",
	"east-us-2.prod.telemetry.example-corp.co.uk.",
	"4d1b5c2e-9f3a-4b7e-8c1d-2a6f0e9b7d34.tracking.some-analytics-vendor.com.",
	"_dns.resolver.arpa.", "_acme-challenge.home.example.org.",
	"1.0.0.127.in-addr.arpa.", "very-long-subdomain.cdn.region.provider.example.co.uk.",
}

var cacheHashBenchSink uint64

// Compare revisions with this same test file, e.g.:
//
//	go test ./resolver -run '^$' -bench '^BenchmarkCacheKeyHash$' -benchmem -count=10
//
// Alternate baseline/candidate runs and compare their distributions with
// benchstat. The timed loop calls the production Hash method directly, with
// input construction, random selection, and per-operation clocks excluded.
func BenchmarkCacheKeyHash(b *testing.B) {
	const doh = "https://dns.nextdns.io/abcdef"
	longName := strings.Repeat(strings.Repeat("a", 63)+".", 3) + strings.Repeat("b", 61) + "."
	for _, tc := range []struct {
		name     string
		contexts []string
		names    []string
	}{
		{"DNS53", []string{""}, cacheHashBenchNames},
		{"DoH", []string{doh}, cacheHashBenchNames},
		{"Mixed", []string{"", doh}, cacheHashBenchNames},
		{"ShortDNS53", []string{""}, []string{"www.example.com."}},
		{"ShortDoH", []string{doh}, []string{"www.example.com."}},
		{"Key255", []string{doh}, []string{longName[:255-len(doh)-4-1] + "."}},
		{"Key256", []string{doh}, []string{longName[:256-len(doh)-4-1] + "."}},
		{"Key257", []string{doh}, []string{longName[:257-len(doh)-4-1] + "."}},
		{"LongName", []string{doh}, []string{longName}},
		{"LongContext", []string{doh + "/" + strings.Repeat("device", 50)}, cacheHashBenchNames},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var keys []cacheKey
			for _, name := range tc.names {
				for _, ctx := range tc.contexts {
					for _, qtype := range []query.Type{query.TypeA, query.TypeAAAA} {
						keys = append(keys, cacheKey{ctx, query.ClassINET, qtype, name})
					}
				}
			}
			b.ReportAllocs()
			var sum uint64
			i := 0
			for b.Loop() {
				sum ^= keys[i].Hash()
				i++
				if i == len(keys) {
					i = 0
				}
			}
			cacheHashBenchSink = sum
		})
	}
}
