//go:build prometheus
// +build prometheus

package metrics

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/nextdns/nextdns/host"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const udpClientWindow = 5 * time.Minute

var (
	totalQueryCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nextdns_queries_total",
		Help: "Total DNS queries processed.",
	})
	totalCacheHitCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nextdns_cache_hits_total",
		Help: "Total DNS cache hits.",
	})
	totalCacheMissCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nextdns_cache_misses_total",
		Help: "Total DNS cache misses.",
	})
	totalCacheExpiredCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nextdns_cache_expired_total",
		Help: "Total DNS cache lookups that found expired entries.",
	})
	totalCacheSizeBytesGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_cache_size_bytes_total",
		Help: "Current size of the DNS cache in bytes.",
	})
	totalCacheSizeKeysGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_cache_size_keys_total",
		Help: "Current count of the DNS cache keys.",
	})
	totalTCPQueryDurationHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nextdns_tcp_query_duration_seconds",
		Help:    "Histogram of total DNS query duration including network latency (TCP/DoH).",
		Buckets: prometheus.DefBuckets,
	})
	totalUDPQueryDurationHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nextdns_udp_query_duration_seconds",
		Help:    "Histogram of total DNS query duration including network latency (UDP).",
		Buckets: prometheus.DefBuckets,
	})
	cacheResponseDurationHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nextdns_cache_response_duration_seconds",
		Help:    "Histogram of cache response durations (seconds).",
		Buckets: prometheus.DefBuckets,
	})
	upstreamTCPResponseDurationHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nextdns_upstream_tcp_response_duration_seconds",
		Help:    "Histogram of upstream TCP response durations (seconds).",
		Buckets: prometheus.DefBuckets,
	})
	upstreamUDPResponseDurationHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nextdns_upstream_udp_response_duration_seconds",
		Help:    "Histogram of upstream UDP response durations (seconds).",
		Buckets: prometheus.DefBuckets,
	})
	localInflightGaugeTCP = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_inflight_queries_local_tcp",
		Help: "Current number of in-flight DNS queries from local TCP clients.",
	})
	localInflightGaugeUDP = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_inflight_queries_local_udp",
		Help: "Current number of in-flight DNS queries from local UDP clients.",
	})
	localInflightGaugeTCPMax = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_inflight_queries_local_tcp_max",
		Help: "Maximum number of in-flight DNS queries from local TCP clients.",
	})
	localInflightGaugeUDPMax = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_inflight_queries_local_udp_max",
		Help: "Maximum number of in-flight DNS queries from local UDP clients.",
	})
	localClientsGaugeUDP = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_unique_clients_udp",
		Help: fmt.Sprintf("Number of unique UDP client IPs seen in the last %d minutes.", int(udpClientWindow.Minutes())),
	})
	upstreamIdleConnGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_idle_connections_upstream",
		Help: "Current number of idle upstream connections.",
	})
	upstreamInflightGaugeTCP = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_inflight_queries_upstream_tcp",
		Help: "Current number of in-flight DNS queries to upstreams (DoH).",
	})
	upstreamInflightGaugeUDP = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nextdns_inflight_queries_upstream_udp",
		Help: "Current number of in-flight DNS queries to upstreams (DNS53).",
	})
)

var (
	udpClientsMu sync.Mutex
	udpClients   = make(map[string]time.Time)

	// InflightTCP and InflightUDP are global counters for protocol-specific in-flight tracking.
	InflightTCP int64
	InflightUDP int64

	// localMaxInflightTCP and localMaxInflightUDP track the maximum number of local
	// in-flight queries for TCP and UDP.
	localMaxInflightTCP float64
	localMaxInflightUDP float64
)

func Init() {
	prometheus.MustRegister(totalQueryCounter)
	prometheus.MustRegister(totalCacheHitCounter)
	prometheus.MustRegister(totalCacheMissCounter)
	prometheus.MustRegister(totalCacheExpiredCounter)
	prometheus.MustRegister(totalCacheSizeBytesGauge)
	prometheus.MustRegister(totalCacheSizeKeysGauge)
	prometheus.MustRegister(totalTCPQueryDurationHistogram)
	prometheus.MustRegister(totalUDPQueryDurationHistogram)
	prometheus.MustRegister(cacheResponseDurationHistogram)
	prometheus.MustRegister(upstreamTCPResponseDurationHistogram)
	prometheus.MustRegister(upstreamUDPResponseDurationHistogram)
	prometheus.MustRegister(localInflightGaugeTCP)
	prometheus.MustRegister(localInflightGaugeUDP)
	prometheus.MustRegister(localInflightGaugeTCPMax)
	prometheus.MustRegister(localInflightGaugeUDPMax)
	prometheus.MustRegister(localClientsGaugeUDP)
	prometheus.MustRegister(upstreamIdleConnGauge)
	prometheus.MustRegister(upstreamInflightGaugeTCP)
	prometheus.MustRegister(upstreamInflightGaugeUDP)
}

func IncQueries() {
	totalQueryCounter.Inc()
}

func IncCacheHit() {
	totalCacheHitCounter.Inc()
}

func IncCacheMiss() {
	totalCacheMissCounter.Inc()
}

func IncCacheExpired() {
	totalCacheExpiredCounter.Inc()
}

func SetCacheSizeBytes(n int) {
	totalCacheSizeBytesGauge.Set(float64(n))
}

func SetCacheSizeKeys(n int) {
	totalCacheSizeKeysGauge.Set(float64(n))
}

func SetIdleConnections(n int) {
	upstreamIdleConnGauge.Set(float64(n))
}

func IncUpstreamInflightTCP() {
	upstreamInflightGaugeTCP.Inc()
}
func DecUpstreamInflightTCP() {
	upstreamInflightGaugeTCP.Dec()
}
func SetUpstreamInflightTCP(n int) {
	upstreamInflightGaugeTCP.Set(float64(n))
}

func IncUpstreamInflightUDP() {
	upstreamInflightGaugeUDP.Inc()
}
func DecUpstreamInflightUDP() {
	upstreamInflightGaugeUDP.Dec()
}
func SetUpstreamInflightUDP(n int) {
	upstreamInflightGaugeUDP.Set(float64(n))
}

func SetInflightTCP(n int) {
	localInflightGaugeTCP.Set(float64(n))
	if float64(n) > localMaxInflightTCP {
		localMaxInflightTCP = float64(n)
		localInflightGaugeTCPMax.Set(localMaxInflightTCP)
	}
}

func SetInflightUDP(n int) {
	localInflightGaugeUDP.Set(float64(n))
	if float64(n) > localMaxInflightUDP {
		localMaxInflightUDP = float64(n)
		localInflightGaugeUDPMax.Set(localMaxInflightUDP)
	}
}

func ObserveTCPQueryDuration(seconds float64) {
	totalTCPQueryDurationHistogram.Observe(seconds)
}

func ObserveUDPQueryDuration(seconds float64) {
	totalUDPQueryDurationHistogram.Observe(seconds)
}

func ObserveCacheResponseDuration(seconds float64) {
	cacheResponseDurationHistogram.Observe(seconds)
}

func ObserveTCPUpstreamResponseDuration(seconds float64) {
	upstreamTCPResponseDurationHistogram.Observe(seconds)
}

func ObserveUDPUpstreamResponseDuration(seconds float64) {
	upstreamUDPResponseDurationHistogram.Observe(seconds)
}

// Call this for every UDP query with the client IP
func RecordUDPClient(ip net.IP) {
	if ip == nil {
		return
	}
	key := ip.String()
	now := time.Now()
	udpClientsMu.Lock()
	udpClients[key] = now
	// Prune old entries
	cutoff := now.Add(-udpClientWindow)
	for k, t := range udpClients {
		if t.Before(cutoff) {
			delete(udpClients, k)
		}
	}
	localClientsGaugeUDP.Set(float64(len(udpClients)))
	udpClientsMu.Unlock()
}

// Serve starts Prometheus metrics HTTP server(s) on the given addresses.
func Serve(addrs []string, log host.Logger) {
	if len(addrs) == 0 {
		addrs = []string{"127.0.0.1:9090"}
	}
	for _, addr := range addrs {
		go func(addr string) {
			defer func() {
				if r := recover(); r != nil {
					if log != nil {
						log.Errorf("Prometheus metrics server panic on %s: %v", addr, r)
					}
				}
			}()
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.Handler())
			if err := http.ListenAndServe(addr, mux); err != nil {
				if log != nil {
					log.Errorf("Prometheus metrics server error on %s: %v", addr, err)
				}
			}
		}(addr)
		if log != nil {
			log.Infof("Prometheus metrics enabled on http://%s/metrics", addr)
		}
	}
}

// EstimateCacheEntrySize estimates the size in bytes of a cache entry (key + value).
// This is a best-effort, not exact, but should be close enough for monitoring.
func EstimateCacheEntrySize(key, value interface{}) int {
	// Estimate key size
	keySize := 0
	switch k := key.(type) {
	case string:
		keySize = len(k)
	case []byte:
		keySize = len(k)
	default:
		keySize = 32 // fallback guess
	}

	// Estimate value size
	valueSize := 0
	switch v := value.(type) {
	case string:
		valueSize = len(v)
	case []byte:
		valueSize = len(v)
	case interface{ Size() int }:
		valueSize = v.Size()
	default:
		valueSize = 128 // fallback guess
	}

	return keySize + valueSize
}
