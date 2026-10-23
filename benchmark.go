package main

import (
	"flag"
	"fmt"
	"runtime"
	"time"

	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

// benchNames approximates real-world query names: mostly short to medium
// popular domains, with a tail of long CDN, telemetry and deep subdomain names.
var benchNames = []string{
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

// benchCtxs are the key contexts in use: empty for DNS53 forwarders, the
// profile URL for DoH.
var benchCtxs = []string{"", "https://dns.nextdns.io/abcdef"}

// benchHash returns the average ns per key hash for algo.
func benchHash(algo resolver.CacheHash, names []string, d time.Duration) (nsPerOp float64, sink uint64) {
	const batch = 1000
	var ops, n int
	start := time.Now()
	for time.Since(start) < d {
		for i := 0; i < batch; i++ {
			// Multiplicative scrambling avoids a sequential access pattern.
			idx := int((uint64(n) * 2654435761) % uint64(len(names)))
			n++
			sink ^= resolver.HashKey(algo, benchCtxs[i&1],
				query.ClassINET, query.TypeA, names[idx])
		}
		ops += batch
	}
	return float64(time.Since(start).Nanoseconds()) / float64(ops), sink
}

func benchmark(args []string) error {
	fs := flag.NewFlagSet("benchmark", flag.ExitOnError)
	dur := fs.Duration("duration", time.Second, "Duration of each measurement.")
	rounds := fs.Int("rounds", 3, "Number of alternating rounds; the best result is kept.")
	fs.Parse(args[1:])
	names := benchNames

	algos := []resolver.CacheHash{resolver.CacheHashXXHash, resolver.CacheHashCRC32}
	best := make([]float64, len(algos))
	var sink uint64
	for r := 0; r < *rounds; r++ {
		for i, a := range algos {
			ns, s := benchHash(a, names, *dur)
			sink ^= s
			if r == 0 || ns < best[i] {
				best[i] = ns
			}
		}
	}
	_ = sink

	fmt.Printf("Platform: %s/%s, %d CPU, %d names\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), len(names))
	for i, a := range algos {
		fmt.Printf("  %-8s %8.1f ns/op\n", a, best[i])
	}
	winner := resolver.CacheHashXXHash
	if best[1] < best[0] {
		winner = resolver.CacheHashCRC32
	}
	fmt.Printf("Faster: %s\n", winner)
	fmt.Printf("To use it: nextdns config set -cache-hash=%s\n", winner)
	return nil
}
