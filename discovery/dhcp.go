package discovery

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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

func (r *Resolver) startDHCP(ctx context.Context, entries chan entry) error {
	file, format := findLeaseFile()
	if file == "" {
		return nil
	}

	if err := readLease(file, format, entries); err != nil && r.WarnLog != nil {
		r.WarnLog(fmt.Sprintf("readLease(%s, %s): %v", file, format, err))
	}
	go func() {
		for {
			select {
			case <-time.After(30 * time.Second):
				if err := readLease(file, format, entries); err != nil && r.WarnLog != nil {
					r.WarnLog(fmt.Sprintf("readLease(%s, %s): %v", file, format, err))
				}
			case <-ctx.Done():
				break
			}
		}
	}()
	return nil
}

func findLeaseFile() (string, string) {
	for _, lease := range leaseFiles {
		if _, err := os.Stat(lease.file); err == nil {
			return lease.file, lease.format
		}
	}
	return "", ""
}

func readLease(file, format string, ch chan entry) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	var entries []entry
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
	for _, entry := range entries {
		ch <- entry
	}
	return nil
}

func readDHCPDLease(r io.Reader) (entries []entry, err error) {
	s := bufio.NewScanner(r)
	var name, ip, mac string
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "}") {
			if name != "" {
				if ip != "" {
					entries = append(entries, entry{sourceDHCP, ip, name})
				}
				if mac != "" {
					entries = append(entries, entry{sourceDHCP, mac, name})
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
			ip = fields[1]
		case "hardware":
			if len(fields) >= 3 {
				mac = strings.TrimRight(fields[2], ";")
			}
		case "client-hostname":
			name = strings.Trim(fields[1], `";`)
		}
	}
	return entries, s.Err()
}

func readDNSMasqLease(r io.Reader) (entries []entry, err error) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 5 {
			entries = append(entries,
				entry{sourceDHCP, fields[1], fields[3]}, // MAC
				entry{sourceDHCP, fields[2], fields[3]}) // IP
		}
	}
	return entries, s.Err()
}
