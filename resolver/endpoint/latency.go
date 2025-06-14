package endpoint

import (
	"context"
	"net"
	"time"
)

// MeasureLatency returns the minimum latency to any of the bootstrap IPs for the endpoint.
func MeasureLatency(ctx context.Context, hostname string, ips []string) (time.Duration, string, error) {
	var minLatency time.Duration
	var fastestIP string
	var firstErr error
	for _, ip := range ips {
		addr := net.JoinHostPort(ip, "443")
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		latency := time.Since(start)
		if err == nil {
			conn.Close()
			if minLatency == 0 || latency < minLatency {
				minLatency = latency
				fastestIP = ip
			}
		} else if firstErr == nil {
			firstErr = err
		}
	}
	if fastestIP == "" && firstErr != nil {
		return 0, "", firstErr
	}
	return minLatency, fastestIP, nil
}
