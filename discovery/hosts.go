package discovery

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

var hostsFiles = []string{
	"/etc/hosts.dnsmasq",
	"/tmp/hosts/dhcp.cfg01411c", // OpenWRT
	`C:\Windows\System32\Drivers\etc\hosts`,
	"/etc/hosts",
}

type Hosts struct {
	OnError func(err error)

	mu       sync.RWMutex
	addrs    map[string][]string
	names    map[string][]string
	fileInfo fileInfo
	expires  time.Time
}

func (r *Hosts) refreshLocked() {
	now := time.Now()
	if now.Before(r.expires) {
		return
	}
	r.expires = now.Add(5 * time.Second)

	file := findHostsFile()
	if file == "" {
		return
	}

	if err := r.readHostsLocked(file); err != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("readHosts(%s): %v", file, err))
	}
}

func (r *Hosts) Name() string {
	return "hosts"
}

func (r *Hosts) Visit(f func(name string, addrs []string)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	for name, addrs := range r.names {
		f(name, addrs)
	}
}

func (r *Hosts) LookupAddr(addr string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.addrs[addr]
}

func (r *Hosts) LookupHost(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.names[prepareHostLookup(name)]
}

func findHostsFile() string {
	for _, file := range hostsFiles {
		if _, err := os.Stat(file); err == nil {
			return file
		}
	}
	return ""
}

func (r *Hosts) readHostsLocked(file string) (err error) {
	if r.fileInfo.Equal(file) {
		return nil
	}

	names, addrs, err := readHostsFile(file)
	if err != nil {
		return err
	}

	r.names = names
	r.addrs = addrs
	r.fileInfo, err = getFileInfo(file)
	return err
}

func readHostsFile(file string) (names, addrs map[string][]string, err error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	names = map[string][]string{}
	addrs = map[string][]string{}

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			// Discard comments.
			line = line[0:i]
		}
		flds := strings.Fields(line)
		if len(flds) < 2 {
			continue
		}
		addr := parseLiteralIP(flds[0])
		if addr == "" {
			continue
		}
		for i := 1; i < len(flds); i++ {
			name := absDomainName([]byte(flds[i]))
			h := []byte(flds[i])
			lowerASCIIBytes(h)
			key := absDomainName(h)
			names[key] = append(names[key], addr)
			addrs[addr] = append(addrs[addr], name)
		}
	}
	for _, lh := range []string{"localhost", "localhost.localdomain."} {
		if len(names[lh]) == 0 {
			// Some systemd based systems like arch linux have an empty hosts
			// file and rely on systemd-resolved to handle special hostnames
			// like localhost. As we don't want to rely on systemd, we have to
			// handle this special case by ourselves. We still let the system
			// redefine those hosts if deemed necessary.
			names[lh] = []string{"127.0.0.1", "::1"}
		}
	}

	return names, addrs, s.Err()
}

func parseLiteralIP(addr string) string {
	ip, zone := parseIPZone(addr)
	if ip == nil {
		return ""
	}
	if zone == "" {
		return ip.String()
	}
	return ip.String() + "%" + zone
}

// parseIPZone parses s as an IP address, return it and its associated zone
// identifier (IPv6 only).
func parseIPZone(s string) (net.IP, string) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.':
			return net.ParseIP(s), ""
		case ':':
			return parseIPv6Zone(s)
		}
	}
	return nil, ""
}

// parseIPv6Zone parses s as a literal IPv6 address and its associated zone
// identifier which is described in RFC 4007.
func parseIPv6Zone(s string) (net.IP, string) {
	s, zone := splitHostZone(s)
	return net.ParseIP(s), zone
}

func splitHostZone(s string) (host, zone string) {
	// The IPv6 scoped addressing zone identifier starts after the
	// last percent sign.
	if i := strings.LastIndexByte(s, '%'); i > 0 {
		host, zone = s[:i], s[i+1:]
	} else {
		host = s
	}
	return
}
