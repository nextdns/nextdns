package endpoint

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ClientInfo struct {
	ID    string
	IP    string
	Model string
	Name  string
}

// Endpoint represents a DoH  server endpoint.
type DOHEndpoint struct {
	// Hostname use to contact the DoH server. If Bootstrap is provided,
	// Hostname is only used for TLS verification.
	Hostname string

	// Path to use with DoH HTTP requests. If empty, the path received in the
	// request by Transport is left untouched.
	Path string

	// Bootstrap is the IPs to use to contact the DoH server. When provided, no
	// DNS request is necessary to contact the DoH server. The fastest IP is
	// used.
	Bootstrap []string `json:"ips"`

	// ALPN is the list of alpn-id declared to be supported by the endpoint
	// through HTTPSSVC or Alt-Svc. If missing, h2 is assumed.
	ALPN []string

	// DoH3Supported caches whether this endpoint supports DoH3 (HTTP/3).
	DoH3Supported bool

	// FastestIP is the currently preferred IP for this endpoint, based on latency probing.
	FastestIP string

	// DebugLog is getting verbose logs if set.
	DebugLog func(msg string)

	once      sync.Once
	transport http.RoundTripper
	onConnect func(*ConnectInfo)
}

func (e *DOHEndpoint) Protocol() Protocol {
	return ProtocolDOH
}

func (e *DOHEndpoint) Equal(e2 Endpoint) bool {
	if e2, ok := e2.(*DOHEndpoint); ok {
		if e.Hostname != e2.Hostname || e.Path != e2.Path || len(e.Bootstrap) != len(e2.Bootstrap) {
			return false
		}
		for i := range e.Bootstrap {
			if e.Bootstrap[i] != e2.Bootstrap[i] {
				return false
			}
		}
		return true
	}
	return false
}

func (e *DOHEndpoint) String() string {
	if len(e.Bootstrap) != 0 {
		return fmt.Sprintf("https://%s%s#%s", e.Hostname, e.Path, strings.Join(e.Bootstrap, ","))
	}
	return fmt.Sprintf("https://%s%s", e.Hostname, e.Path)
}

func (e *DOHEndpoint) Exchange(ctx context.Context, payload, buf []byte) (n int, err error) {
	var start time.Time
	var dur time.Duration
	url := "https://" + e.Hostname + e.Path
	if e.DebugLog != nil {
		start = time.Now()
		e.debugf("[DoH] Starting query to %s (DoH3=%v, FastestIP=%s)", e.Hostname, e.DoH3Supported, e.FastestIP)
		e.debugf("[DoH] Request URL: %s", url)
		e.debug("[DoH] Request headers: Content-Type: application/dns-message")
		e.debugf("[DoH] Request payload size: %d bytes", len(payload))
		// Try to parse the DNS query name/type for logging
		if len(payload) > 12 {
			qname, qtype := parseDNSQuery(payload)
			e.debugf("[DoH] DNS Query: name=%s, type=%s", qname, qtype)
		}
	}
	req, _ := http.NewRequest("POST", url, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/dns-message")
	req = req.WithContext(ctx)
	res, err := e.RoundTrip(req)
	var proto string
	if res != nil {
		proto = res.Proto
	}
	if e.DebugLog != nil {
		dur = time.Since(start)
		e.debugf("[DoH] Response status: %s (%d), Protocol: %s", res.Status, res.StatusCode, proto)
	}
	if err != nil {
		if dur != 0 {
			e.debugf("[DoH] Query to %s (DoH3=%v, FastestIP=%s) failed after %v: %v, Protocol: %s", e.Hostname, e.DoH3Supported, e.FastestIP, dur, err, proto)
		} else {
			e.debugf("[DoH] Query to %s (DoH3=%v, FastestIP=%s) failed: %v, Protocol: %s", e.Hostname, e.DoH3Supported, e.FastestIP, err, proto)
		}
		var uaeErr x509.UnknownAuthorityError
		if errors.As(err, &uaeErr) {
			return 0, fmt.Errorf("roundtrip: %v (subject=%v, issuer=%v)",
				err, uaeErr.Cert.Subject, uaeErr.Cert.Issuer)
		}
		return 0, fmt.Errorf("roundtrip: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		if e.DoH3Supported {
			e.debugf("[DoH] Query to %s (DoH3=%v, FastestIP=%s) got HTTP %d after %v, Protocol: %s", e.Hostname, e.DoH3Supported, e.FastestIP, res.StatusCode, dur, proto)
		} else {
			if dur != 0 {
				e.debugf("[DoH] Query to %s got HTTP %d after %v, Protocol: %s", e.Hostname, res.StatusCode, dur, proto)
			} else {
				e.debugf("[DoH] Query to %s got HTTP %d, Protocol: %s", e.Hostname, res.StatusCode, proto)
			}
		}
		return 0, fmt.Errorf("status: %d", res.StatusCode)
	}
	n, err = res.Body.Read(buf)
	e.debugf("[DoH] Response size: %d bytes", n)
	if err != nil && !errors.Is(err, io.EOF) {
		e.debugf("[DoH] Query to %s (DoH3=%v, FastestIP=%s) read error after %v: %v", e.Hostname, e.DoH3Supported, e.FastestIP, dur, err)
		return n, fmt.Errorf("read: %v", err)
	}
	if e.DoH3Supported {
		e.debugf("[DoH] Query to %s (DoH3=%v, FastestIP=%s) succeeded in %v", e.Hostname, e.DoH3Supported, e.FastestIP, dur)
	} else {
		e.debugf("[DoH] Query to %s succeeded in %v", e.Hostname, dur)
	}
	return n, nil
}

// parseDNSQuery attempts to extract the query name and type from a DNS wire payload for logging
func parseDNSQuery(payload []byte) (name string, qtype string) {
	if len(payload) < 14 {
		return "", ""
	}
	// DNS header is 12 bytes, question starts at 12
	off := 12
	labels := []string{}
	for off < len(payload) && payload[off] != 0 {
		l := int(payload[off])
		off++
		if off+l > len(payload) {
			break
		}
		labels = append(labels, string(payload[off:off+l]))
		off += l
	}
	name = strings.Join(labels, ".")
	if off+5 <= len(payload) {
		typeCode := int(payload[off+1])<<8 | int(payload[off+2])
		switch typeCode {
		case 1:
			qtype = "A"
		case 28:
			qtype = "AAAA"
		case 15:
			qtype = "MX"
		case 16:
			qtype = "TXT"
		case 33:
			qtype = "SRV"
		case 255:
			qtype = "ANY"
		default:
			qtype = fmt.Sprintf("TYPE%d", typeCode)
		}
	}
	return
}

func (e *DOHEndpoint) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	e.once.Do(func() {
		if e.transport == nil {
			addrs := endpointAddrs(e)
			if e.DoH3Supported {
				// If using HTTP/3 and the hostname does not already begin with "doh3.", prepend it.
				if !strings.HasPrefix(strings.ToLower(e.Hostname), "doh3.") {
					e.Hostname = "doh3." + e.Hostname
				}
				e.transport = newTransportH3(e, addrs)
			} else {
				e.transport = newTransportH2(e, addrs)
			}
		}
	})
	return e.transport.RoundTrip(req)
}

// endpointAddrs returns the list of addresses for a DOHEndpoint, prioritizing FastestIP if set.
// This version ensures FastestIP is first, with port, and all others follow (with port).
func endpointAddrs(e *DOHEndpoint) []string {
	addrs := make([]string, 0, len(e.Bootstrap))
	for _, ip := range e.Bootstrap {
		if !strings.Contains(ip, ":") {
			ip = net.JoinHostPort(ip, "443")
		}
		addrs = append(addrs, ip)
	}
	if e.FastestIP != "" {
		fastest := e.FastestIP
		if !strings.Contains(fastest, ":") {
			fastest = net.JoinHostPort(fastest, "443")
		}
		filtered := make([]string, 0, len(addrs))
		for _, a := range addrs {
			if a != fastest {
				filtered = append(filtered, a)
			}
		}
		addrs = append([]string{fastest}, filtered...)
	}
	return addrs
}

func (e *DOHEndpoint) debug(msg string) {
	if e.DebugLog != nil {
		e.DebugLog(msg)
	}
}

func (e *DOHEndpoint) debugf(format string, a ...interface{}) {
	if e.DebugLog != nil {
		e.DebugLog(fmt.Sprintf(format, a...))
	}
}
