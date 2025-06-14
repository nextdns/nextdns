package endpoint

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	quic "github.com/quic-go/quic-go"
)

// SupportsDoH3 returns true if DoH3 (HTTP/3) is supported for the given endpoint and bootstrap IPs.
// This version always attempts a real DoH3 request, regardless of ALPN.
func SupportsDoH3(endpoint string, bootstrapIPs []string, alpnList []string, debugLog func(string)) bool {
	if debugLog != nil {
		debugLog(fmt.Sprintf("[DoH3] SupportsDoH3 called for endpoint=%s, bootstrapIPs=%v, alpnList=%v", endpoint, bootstrapIPs, alpnList))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := probeDoH3(ctx, endpoint, bootstrapIPs, debugLog); err == nil {
		if debugLog != nil {
			debugLog(fmt.Sprintf("[DoH3] QUIC probe succeeded for endpoint=%s", endpoint))
		}
		return true
	} else if debugLog != nil {
		debugLog(fmt.Sprintf("[DoH3] QUIC probe failed for endpoint=%s: %v, trying real DoH3 request", endpoint, err))
	}
	if err := probeDoH3Request(ctx, endpoint, bootstrapIPs); err == nil {
		if debugLog != nil {
			debugLog(fmt.Sprintf("[DoH3] Real DoH3 request succeeded for endpoint=%s", endpoint))
		}
		return true
	} else if debugLog != nil {
		debugLog(fmt.Sprintf("[DoH3] Real DoH3 request failed for endpoint=%s: %v", endpoint, err))
	}
	return false
}

// alpnIncludesH3 checks if the ALPN string (comma-separated, e.g. "h3,h2") contains "h3".
// This matches the format seen in SVCB/HTTPS records.
// func alpnIncludesH3(alpn string) bool {
// 	for _, proto := range strings.Split(alpn, ",") {
// 		if strings.TrimSpace(proto) == "h3" {
// 			return true
// 		}
// 	}
// 	return false
// }

// probeDoH3 tries to establish a QUIC connection to the endpoint using all bootstrap IPs.
func probeDoH3(ctx context.Context, endpoint string, bootstrapIPs []string, debugLog func(string)) error {
	if len(bootstrapIPs) == 0 {
		if debugLog != nil {
			debugLog(fmt.Sprintf("[DoH3] No bootstrap IPs for endpoint=%s", endpoint))
		}
		return context.DeadlineExceeded
	}
	var lastErr error
	for _, ip := range bootstrapIPs {
		var start time.Time
		var elapsed time.Duration
		addr := net.JoinHostPort(ip, "443")
		if debugLog != nil {
			debugLog(fmt.Sprintf("[DoH3] Attempting QUIC probe: endpoint=%s, ip=%s, addr=%s, SNI=%s", endpoint, ip, addr, endpoint))
			start = time.Now()
		}
		tlsConf := &tls.Config{
			ServerName: endpoint,
			NextProtos: []string{"h3"}, // Ensure ALPN "h3" is offered for probe
		}
		_, err := quic.DialAddrEarly(ctx, addr, tlsConf, nil)
		if err == nil {
			if debugLog != nil {
				elapsed = time.Since(start)
				debugLog(fmt.Sprintf("[DoH3] QUIC probe to %s succeeded in %v", addr, elapsed))
			}
			return nil // success
		}
		if debugLog != nil {
			elapsed = time.Since(start)
			debugLog(fmt.Sprintf("[DoH3] QUIC probe to %s failed after %v: %v", addr, elapsed, err))
		}
		lastErr = err
	}
	return lastErr
}

// probeDoH3Request attempts a real DoH3 DNS request using HTTP/3 to confirm support.
func probeDoH3Request(ctx context.Context, endpoint string, bootstrapIPs []string) error {
	if len(bootstrapIPs) == 0 {
		return context.DeadlineExceeded
	}
	for _, ip := range bootstrapIPs {
		url := "https://" + endpoint + "/dns-query"
		tr := newTransportH3(&DOHEndpoint{Hostname: endpoint}, []string{ip})
		req, _ := http.NewRequestWithContext(ctx, "POST", url, nil)
		req.Header.Set("Content-Type", "application/dns-message")
		resp, err := tr.RoundTrip(req)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	return context.DeadlineExceeded
}
