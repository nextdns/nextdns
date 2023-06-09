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
	macs    map[string][]string
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

func (r *Merlin) Visit(f func(name string, macs []string)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	m := map[string][]string{}
	for mac, names := range r.macs {
		for _, name := range names {
			m[name] = append(m[name], mac)
		}
	}
	for name, macs := range m {
		f(name, macs)
	}
}

func (r *Merlin) LookupMAC(mac string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.macs[mac]
}

func (r *Merlin) LookupAddr(addr string) []string {
	return nil
}

func (r *Merlin) LookupHost(name string) []string {
	return nil
}

func (r *Merlin) clientListLocked() error {
	cmd := exec.Command("nvram", "get", "custom_clientlist")
	b, err := cmd.Output()
	if err != nil {
		return err
	}
	macs, err := readClientList(b)
	if err != nil {
		return err
	}
	r.macs = macs
	return nil
}

func readClientList(b []byte) (macs map[string][]string, err error) {
	if len(b) == 0 {
		return nil, nil
	}

	// Dirty hack - attempt to add '<' char when var is populated, but opening '<' is non-existent
	if len(b) > 0 && b[0] != '<' {
		b = append([]byte{'<'}, b[0:]...)
	}

	macs = map[string][]string{}
	for len(b) > 0 {
		switch b[0] {
		case '<':
			// parse
		case '\n', '\r':
			b = b[1:]
			continue
		default:
			return nil, fmt.Errorf("%s: invalid format: missing item separator", string(b))
		}
		b = b[1:]
		eol := bytes.IndexByte(b, '<')
		if eol == -1 {
			eol = len(b)
		}
		idx := bytes.IndexByte(b, '>')
		if idx == -1 {
			return nil, fmt.Errorf("%s: invalid format: missing host separator", string(b))
		}
		idx2 := idx + 18
		if idx2 > eol || len(b) <= idx2 || b[idx2] != '>' {
			return nil, fmt.Errorf("%s: invalid format: missing MAC separator", string(b))
		}
		if idx > 0 {
			name := string(b[:idx])
			mac := string(bytes.ToLower(b[idx+1 : idx2]))
			macs[mac] = appendUniq(macs[mac], name)
		}
		b = b[eol:]
	}
	return macs, nil
}
