package discovery

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type leaseFile struct {
	file, format string
}

var leaseFiles = []leaseFile{
	{"/var/run/dhcpd.leases", "isc-dhcpd"},
	{"/var/lib/dhcp/dhcpd.leases", "isc-dhcpd"},
	{"/var/lib/misc/dnsmasq.leases", "dnsmasq"},
	{"/tmp/dnsmasq.leases", "dnsmasq"},
	{"/tmp/dhcp.leases", "dnsmasq"},
	{"/etc/dhcpd/dhcpd.conf.leases", "dnsmasq"},
	{"/var/run/dnsmasq-dhcp.leases", "dnsmasq"},
}

type DHCP struct {
	mu sync.RWMutex
	m  map[string]string
}

func (r *DHCP) Start(ctx context.Context) error {
	file, format := findLeaseFile()
	if file == "" {
		return nil
	}

	t := TraceFromCtx(ctx)
	if err := r.readLease(ctx, file, format); err != nil && t.OnWarning != nil {
		t.OnWarning(fmt.Sprintf("readLease(%s, %s): %v", file, format, err))
	}
	go func() {
		for {
			select {
			case <-time.After(30 * time.Second):
				if err := r.readLease(ctx, file, format); err != nil && t.OnWarning != nil {
					t.OnWarning(fmt.Sprintf("readLease(%s, %s): %v", file, format, err))
				}
			case <-ctx.Done():
				break
			}
		}
	}()
	return nil
}

func (r *DHCP) Lookup(addr string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, found := r.m[addr]
	return name, found
}

func findLeaseFile() (string, string) {
	for _, lease := range leaseFiles {
		if _, err := os.Stat(lease.file); err == nil {
			return lease.file, lease.format
		}
	}
	return "", ""
}

func (r *DHCP) readLease(ctx context.Context, file, format string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	var entries map[string]string
	switch format {
	case "isc-dhcpd":
		entries, err = readDHCPDLease(f)
	case "dnsmasq":
		entries, err = readDNSMasqLease(f)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
	if err != nil {
		return err
	}
	t := TraceFromCtx(ctx)
	if len(entries) > 0 {
		for addr, name := range entries {
			r.mu.Lock()
			if r.m[addr] != name {
				if r.m == nil {
					r.m = map[string]string{}
				}
				r.m[addr] = name
				r.mu.Unlock()
				if t.OnDiscover != nil {
					t.OnDiscover(addr, name, "DHCP")
				}
			} else {
				r.mu.Unlock()
			}
		}
	}
	return nil
}

func readDHCPDLease(r io.Reader) (map[string]string, error) {
	s := bufio.NewScanner(r)
	var name, ip, mac string
	entries := map[string]string{}
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "}") {
			if name != "" {
				if ip != "" {
					entries[ip] = name
				}
				if mac != "" {
					entries[mac] = name
				}
			}
			name, ip, mac = "", "", ""
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "lease":
			ip = strings.ToLower(fields[1])
		case "hardware":
			if len(fields) >= 3 {
				mac = strings.ToLower(strings.TrimRight(fields[2], ";"))
			}
		case "client-hostname":
			name = normalizeName(strings.Trim(fields[1], `";`))
		}
	}
	return entries, s.Err()
}

func readDNSMasqLease(r io.Reader) (map[string]string, error) {
	s := bufio.NewScanner(r)
	entries := map[string]string{}
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 5 {
			name := normalizeName(fields[3])
			entries[strings.ToLower(fields[1])] = name // MAC
			entries[strings.ToLower(fields[2])] = name // IP
		}
	}
	return entries, s.Err()
}
