package discovery

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Merlin struct {
	OnError func(err error)

	once      sync.Once
	supported bool

	mu      sync.RWMutex
	addrs   map[string][]string
	names   map[string][]string
	expires time.Time
}

func (r *Merlin) init() {
	b, _ := exec.Command("uname", "-o").Output()
	if strings.HasPrefix(string(b), "ASUSWRT-Merlin") {
		r.supported = true
	}
}

func (r *Merlin) refreshLocked() {
	r.once.Do(r.init)
	if !r.supported {
		return
	}

	now := time.Now()
	if now.Before(r.expires) {
		return
	}
	r.expires = now.Add(30 * time.Second)

	if err := r.clientListLocked(); err != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("clientList: %v", err))
	}
}

func (r *Merlin) Name() string {
	return "merlin"
}

func (r *Merlin) Visit(f func(name string, addrs []string)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	for name, addrs := range r.names {
		f(name, addrs)
	}
}

func (r *Merlin) LookupAddr(addr string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.addrs[addr]
}

func (r *Merlin) LookupHost(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.names[prepareHostLookup(name)]
}

func (r *Merlin) clientListLocked() error {
	cmd := exec.Command("nvram", "get", "custom_clientlist")
	b, err := cmd.Output()
	if err != nil {
		return err
	}
	names, addrs, err := readClientList(b)
	if err != nil {
		return err
	}
	r.names, r.addrs = names, addrs
	return nil
}

func readClientList(b []byte) (names, addrs map[string][]string, err error) {
	if len(b) == 0 {
		return nil, nil, nil
	}
	names, addrs = map[string][]string{}, map[string][]string{}
	for len(b) > 0 {
		switch b[0] {
		case '<':
			// parse
		case '\n', '\r':
			b = b[1:]
			continue
		default:
			return nil, nil, fmt.Errorf("%s: invalid format: missing item separator", string(b))
		}
		b = b[1:]
		eol := bytes.IndexByte(b, '<')
		if eol == -1 {
			eol = len(b)
		}
		idx := bytes.IndexByte(b, '>')
		if idx == -1 {
			return nil, nil, fmt.Errorf("%s: invalid format: missing host separator", string(b))
		}
		idx2 := idx + 18
		if idx2 > eol || len(b) <= idx2 || b[idx2] != '>' {
			return nil, nil, fmt.Errorf("%s: invalid format: missing MAC separator", string(b))
		}
		if idx > 0 {
			name := absDomainName(b[:idx])
			h := b[:idx]
			lowerASCIIBytes(h)
			key := absDomainName(h)
			addr := string(bytes.ToLower(b[idx+1 : idx2]))
			names[key] = appendUniq(names[key], addr)
			names[key+"local."] = appendUniq(names[key], addr)
			addrs[addr] = appendUniq(addrs[addr], name)
		}
		b = b[eol:]
	}
	return names, addrs, nil
}
