package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type Resolver struct {
	mu   sync.RWMutex
	m    map[string]string
	miss func(addr string)

	OnDiscover func(addr, host, source string)

	WarnLog func(string)
}

type entry struct {
	source, addr, name string
}

var (
	sourceMDNS = "mdns"
	sourceDHCP = "dhcp"
	sourceDNS  = "dns"
)

func (r *Resolver) Start(ctx context.Context) {
	entries := make(chan entry)

	go r.run(ctx, entries)

	if err := r.startMDNS(ctx, entries); err != nil {
		if r.WarnLog != nil {
			r.WarnLog(fmt.Sprintf("mdns: %v", err))
		}
	}
	if err := r.startDHCP(ctx, entries); err != nil {
		if r.WarnLog != nil {
			r.WarnLog(fmt.Sprintf("dhcp: %v", err))
		}
	}
	if err := r.startDNS(ctx, entries); err != nil {
		if r.WarnLog != nil {
			r.WarnLog(fmt.Sprintf("dns: %v", err))
		}
	}
}

func (r *Resolver) Lookup(addr string) string {
	addr = strings.ToLower(addr)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name, found := r.m[sourceMDNS+addr]; found {
		return name
	}
	if name, found := r.m[sourceDHCP+addr]; found {
		return name
	}
	if name, found := r.m[sourceDNS+addr]; found {
		return name
	}
	if r.miss != nil {
		r.miss(addr)
	}
	return ""
}

func (r *Resolver) run(ctx context.Context, ch chan entry) {
	for entry := range ch {
		r.mu.Lock()
		if r.m == nil {
			r.m = map[string]string{}
		}
		name := entry.name
		if idx := strings.IndexByte(name, '.'); idx != -1 {
			name = name[:idx] // remove .local. suffix
		}
		if !isValidName(name) {
			r.mu.Unlock()
			continue
		}
		addr := entry.source + strings.ToLower(entry.addr)
		if r.m[addr] != name {
			r.m[addr] = name
			r.mu.Unlock()
			if r.OnDiscover != nil {
				r.OnDiscover(entry.addr, name, entry.source)
			}
		} else {
			r.mu.Unlock()
		}
	}
}

func isValidName(name string) bool {
	if name == "" {
		return false
	}
	// ignore 331e87e5-3018-5336-23f3-595cdea48d9b
	if len(name) == 36 &&
		name[8] == '-' && name[13] == '-' && name[18] == '-' && name[23] == '-' &&
		strings.Trim(name, "0123456789abcdef-") == "" {
		return false
	}
	// ignore CC_22_3D_E4_CE_FE
	if len(name) == 17 &&
		name[2] == '_' && name[5] == '_' && name[8] == '_' && name[11] == '_' && name[14] == '_' &&
		strings.Trim(name, "0123456789ABCDEF_") == "" {
		return false
	}
	// ignore 10-0-0-213
	if len(name) >= 7 && len(name) <= 15 &&
		strings.Trim(name, "0123456789-") == "" {
		return false
	}

	return true
}
