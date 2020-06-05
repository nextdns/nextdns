package discovery

import (
	"bufio"
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
	{"/var/dhcpd/var/db/dhcpd.leases", "isc-dhcpd"},
	{"/var/lib/misc/dnsmasq.leases", "dnsmasq"},
	{"/tmp/dnsmasq.leases", "dnsmasq"},
	{"/tmp/dhcp.leases", "dnsmasq"},
	{"/etc/dhcpd/dhcpd.conf.leases", "dnsmasq"},
	{"/var/run/dnsmasq-dhcp.leases", "dnsmasq"},
	{"/config/dhcpd.leases", "dnsmasq"},
}

type DHCP struct {
	OnError func(err error)

	mu       sync.RWMutex
	macs     map[string][]string
	addrs    map[string][]string
	names    map[string][]string
	fileInfo fileInfo
	expires  time.Time
}

func (r *DHCP) refreshLocked() {
	now := time.Now()
	if now.Before(r.expires) {
		return
	}
	r.expires = now.Add(5 * time.Second)

	file, format := findLeaseFile()
	if file == "" {
		return
	}

	if err := r.readLeaseLocked(file, format); err != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("readLease(%s, %s): %v", file, format, err))
	}
}

func (r *DHCP) Name() string {
	return "dhcp"
}

func (r *DHCP) Visit(f func(name string, addrs []string)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	for name, addrs := range r.names {
		f(name, addrs)
	}
}

func (r *DHCP) LookupMAC(mac string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.macs[mac]
}

func (r *DHCP) LookupAddr(addr string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.addrs[addr]
}

func (r *DHCP) LookupHost(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.names[prepareHostLookup(name)]
}

func findLeaseFile() (string, string) {
	for _, lease := range leaseFiles {
		if _, err := os.Stat(lease.file); err == nil {
			return lease.file, lease.format
		}
	}
	return "", ""
}

func (r *DHCP) readLeaseLocked(file, format string) error {
	if r.fileInfo.Equal(file) {
		return nil
	}

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	var macs, addrs, names map[string][]string
	switch format {
	case "isc-dhcpd":
		macs, addrs, names, err = readDHCPDLease(f)
	case "dnsmasq":
		macs, addrs, names, err = readDNSMasqLease(f)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
	if err != nil {
		return err
	}
	r.macs = macs
	r.addrs = addrs
	r.names = names
	r.fileInfo, err = getFileInfo(file)
	return err
}

func readDHCPDLease(r io.Reader) (macs, addrs, names map[string][]string, err error) {
	s := bufio.NewScanner(r)
	var name, ip, mac string
	macs, addrs, names = map[string][]string{}, map[string][]string{}, map[string][]string{}
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "}") {
			if name != "" {
				name := absDomainName([]byte(name))
				if ip != "" {
					h := []byte(name)
					lowerASCIIBytes(h)
					key := absDomainName(h)
					names[key] = appendUniq(names[key], ip)
					names[key+"local."] = appendUniq(names[key+"local."], ip)
					addrs[ip] = appendUniq(addrs[ip], name)
				}
				if mac != "" {
					macs[mac] = appendUniq(macs[mac], name)
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
			name = strings.Trim(fields[1], `";`)
		}
	}
	return macs, addrs, names, s.Err()
}

func readDNSMasqLease(r io.Reader) (macs, addrs, names map[string][]string, err error) {
	s := bufio.NewScanner(r)
	macs, addrs, names = map[string][]string{}, map[string][]string{}, map[string][]string{}
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 5 {
			name := absDomainName([]byte(fields[3]))
			h := []byte(name)
			lowerASCIIBytes(h)
			key := absDomainName(h)
			mac := strings.ToLower(fields[1])
			ip := strings.ToLower(fields[2])
			macs[mac] = appendUniq(macs[mac], name)
			addrs[ip] = appendUniq(addrs[ip], name)
			names[key] = appendUniq(names[key], ip)
			names[key+"local."] = appendUniq(names[key+"local."], ip)
		}
	}
	return macs, addrs, names, s.Err()
}
