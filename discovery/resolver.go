package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type Resolver struct {
	mu sync.RWMutex
	m  map[string]string

	OnDiscover func(addr, host string)

	WarnLog func(string)
}

type entry struct {
	addr, name string
}

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
}

func (r *Resolver) Lookup(addr string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[strings.ToLower(addr)]
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
		addr := strings.ToLower(entry.addr)
		if r.m[addr] != name {
			r.m[addr] = name
			r.mu.Unlock()
			if r.OnDiscover != nil {
				r.OnDiscover(addr, name)
			}
		} else {
			r.mu.Unlock()
		}
	}
}
