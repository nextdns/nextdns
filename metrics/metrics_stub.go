//go:build !prometheus
// +build !prometheus

package metrics

import "github.com/nextdns/nextdns/host"

func Init()                                 {}
func IncQueries()                           {}
func IncCacheHit()                          {}
func IncCacheMiss()                         {}
func SetCacheSizeBytes(n int)               {}
func SetCacheSizeKeys(n int)                {}
func SetIdleConnections(n int)              {}
func Serve(addrs []string, log host.Logger) {}
func RecordUDPClient(ip interface{})        {}
func IncCacheExpired()                      {}

func IncUpstreamInflightTCP()      {}
func DecUpstreamInflightTCP()      {}
func SetUpstreamInflightTCP(n int) {}
func IncUpstreamInflightUDP()      {}
func DecUpstreamInflightUDP()      {}
func SetUpstreamInflightUDP(n int) {}

func SetInflightTCP(n int) {}
func SetInflightUDP(n int) {}

func ObserveTCPQueryDuration(seconds float64)            {}
func ObserveUDPQueryDuration(seconds float64)            {}
func ObserveCacheResponseDuration(seconds float64)       {}
func ObserveTCPUpstreamResponseDuration(seconds float64) {}
func ObserveUDPUpstreamResponseDuration(seconds float64) {}

func EstimateCacheEntrySize(key, value interface{}) int { return 0 }

var InflightTCP int64
var InflightUDP int64
var localMaxInflightTCP float64
var localMaxInflightUDP float64
var localClientsGaugeUDP float64
