//go:build linux

package discovery

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

//go:embed firewalla.lua
var firewallaRedisScript string

type Firewalla struct {
	OnError func(err error)

	once      sync.Once
	supported bool

	mu      sync.RWMutex
	macs    map[string][]string
	expires time.Time
}

func isFirewalla() bool {
	_, err := os.Stat("/etc/firewalla_release")
	return err == nil
}

func (r *Firewalla) init() {
	if isFirewalla() {
		r.supported = true
	}
}

func (r *Firewalla) refreshLocked() {
	r.once.Do(r.init)
	if !r.supported {
		return
	}

	now := time.Now()
	if now.Before(r.expires) {
		return
	}
	r.expires = now.Add(5 * time.Minute)

	if err := r.clientListLocked(); err != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("clientList: %v", err))
	}
}

func (r *Firewalla) Name() string {
	return "firewalla"
}

func (r *Firewalla) Visit(f func(name string, macs []string)) {
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

func (r *Firewalla) LookupMAC(mac string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.macs[mac]
}

func (r *Firewalla) LookupAddr(addr string) []string {
	return nil
}

func (r *Firewalla) LookupHost(name string) []string {
	return nil
}

func (r *Firewalla) clientListLocked() error {
	lfh, err := os.CreateTemp("", "firewalla.lua")
	if err != nil {
		return err
	}
	if _, err = lfh.Write([]byte(firewallaRedisScript)); err != nil {
		return err
	}
	lfh.Close()
	luaScript := lfh.Name()
	defer os.Remove(luaScript)
	cmd := exec.Command("/usr/bin/redis-cli", "--eval", luaScript)
	b, err := cmd.Output()
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	return d.Decode(&r.macs)
}
