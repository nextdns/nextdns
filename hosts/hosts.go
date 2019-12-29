// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hosts

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const cacheMaxAge = 5 * time.Second

var testHookHostsPath = "/etc/hosts"

// LookupHost looks up the addresses for the given host from /etc/hosts.
func LookupHost(host string) []string {
	hosts.Lock()
	defer hosts.Unlock()
	readHosts()
	if len(hosts.byName) != 0 {
		// TODO(jbd,bradfitz): avoid this alloc if host is already all lowercase?
		// or linear scan the byName map if it's small enough?
		lowerHost := []byte(host)
		lowerASCIIBytes(lowerHost)
		if ips, ok := hosts.byName[absDomainName(lowerHost)]; ok {
			ipsCp := make([]string, len(ips))
			copy(ipsCp, ips)
			return ipsCp
		}
	}
	return nil
}

// LookupAddr looks up the hosts for the given address from /etc/hosts.
func LookupAddr(addr string) []string {
	hosts.Lock()
	defer hosts.Unlock()
	readHosts()
	addr = parseLiteralIP(addr)
	if addr == "" {
		return nil
	}
	if len(hosts.byAddr) != 0 {
		if hosts, ok := hosts.byAddr[addr]; ok {
			hostsCp := make([]string, len(hosts))
			copy(hostsCp, hosts)
			return hostsCp
		}
	}
	return nil
}

// hosts contains known host entries.
var hosts struct {
	sync.Mutex

	// Key for the list of literal IP addresses must be a host
	// name. It would be part of DNS labels, a FQDN or an absolute
	// FQDN.
	// For now the key is converted to lower case for convenience.
	byName map[string][]string

	// Key for the list of host names must be a literal IP address
	// including IPv6 address with zone identifier.
	// We don't support old-classful IP address notation.
	byAddr map[string][]string

	expire time.Time
	path   string
	mtime  time.Time
	size   int64
}

func readHosts() {
	now := time.Now()
	hp := testHookHostsPath

	if now.Before(hosts.expire) && hosts.path == hp && len(hosts.byName) > 0 {
		return
	}
	mtime, size, err := stat(hp)
	if err == nil && hosts.path == hp && hosts.mtime.Equal(mtime) && hosts.size == size {
		hosts.expire = now.Add(cacheMaxAge)
		return
	}

	hs := make(map[string][]string)
	is := make(map[string][]string)
	var file *file
	if file, err = open(hp); file == nil {
		fmt.Println("return 1", err)
		return
	}
	for line, ok := file.readLine(); ok; line, ok = file.readLine() {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			// Discard comments.
			line = line[0:i]
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		addr := parseLiteralIP(f[0])
		if addr == "" {
			continue
		}
		for i := 1; i < len(f); i++ {
			name := absDomainName([]byte(f[i]))
			h := []byte(f[i])
			lowerASCIIBytes(h)
			key := absDomainName(h)
			hs[key] = append(hs[key], addr)
			is[addr] = append(is[addr], name)
		}
	}
	for _, lh := range []string{"localhost", "localhost.localdomain."} {
		if len(hs[lh]) == 0 {
			// Some systemd based systems like arch linux have an empty hosts
			// file and rely on systemd-resolved to handle special hostnames
			// like localhost. As we don't want to rely on systemd, we have to
			// handle this special case by ourselves. We still let the system
			// redefine those hosts if deemed necessary.
			hs[lh] = []string{"127.0.0.1", "::1"}
		}
	}
	// Update the data cache.
	hosts.expire = now.Add(cacheMaxAge)
	hosts.path = hp
	hosts.byName = hs
	hosts.byAddr = is
	hosts.mtime = mtime
	hosts.size = size
	file.close()
}
