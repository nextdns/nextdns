package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nextdns/nextdns/arp"
)

type Ubios struct {
	OnError func(err error)

	once      sync.Once
	supported bool

	mu      sync.RWMutex
	macs    map[string][]string
	names   map[string][]string
	expires time.Time
}

func (r *Ubios) init() {
	if st, _ := os.Stat("/etc/unifi-base-ucore"); st != nil && st.IsDir() {
		r.supported = true
	}
}

func (r *Ubios) refreshLocked() {
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

func (r *Ubios) Name() string {
	return "ubios"
}

func (r *Ubios) Visit(f func(name string, macs []string)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	for name, macs := range r.names {
		f(name, macs)
	}
}

func (r *Ubios) LookupMAC(mac string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.macs[mac]
}

func (r *Ubios) LookupAddr(addr string) []string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return nil
	}
	if ip = ip.To4(); ip == nil {
		return nil
	}
	mac := arp.SearchMAC(ip)
	if mac == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.macs[mac.String()]
}

func (r *Ubios) LookupHost(name string) []string {
	r.mu.RLock()
	r.refreshLocked()
	macs := r.names[prepareHostLookup(name)]
	r.mu.RUnlock()
	if len(macs) == 0 {
		return nil
	}
	var ips []string
	for i := range macs {
		mac, err := net.ParseMAC(macs[i])
		if err != nil {
			continue
		}
		ip := arp.SearchIP(mac)
		if ip == nil {
			continue
		}
		ips = append(ips, ip.String())
	}
	return ips
}

func (r *Ubios) clientListLocked() error {
	cmd := exec.Command("/usr/bin/mongo", "localhost:27117/ace", "--quiet", "--eval", `
		DBQuery.shellBatchSize = 1000;
		db.user.find({name: {$exists: true, $ne: ""}}, {_id:0, mac:1, name:1});`)
	b, err := cmd.Output()
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	rec := struct {
		MAC  string
		Name string
	}{}
	names, macs := map[string][]string{}, map[string][]string{}
	for d.Decode(&rec) == nil {
		mac := strings.ToLower(rec.MAC)
		name := strings.ReplaceAll(rec.Name, " ", "-") + ".local."
		key := strings.ToLower(name)
		names[key] = appendUniq(names[key], mac)
		macs[mac] = appendUniq(macs[mac], name)
	}
	r.names, r.macs = names, macs
	return nil
}
