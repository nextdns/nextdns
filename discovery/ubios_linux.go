package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Ubios struct {
	OnError func(err error)

	once      sync.Once
	supported bool

	mu      sync.RWMutex
	macs    map[string][]string
	expires time.Time
}

func (r *Ubios) init() {
	if st, _ := os.Stat("/data/unifi"); st != nil && st.IsDir() {
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

func (r *Ubios) LookupMAC(mac string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.refreshLocked()
	return r.macs[mac]
}

func (r *Ubios) LookupAddr(addr string) []string {
	return nil
}

func (r *Ubios) LookupHost(name string) []string {
	return nil
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
	macs := map[string][]string{}
	for d.Decode(&rec) == nil {
		mac := strings.ToLower(rec.MAC)
		macs[mac] = appendUniq(macs[mac], rec.Name)
	}
	r.macs = macs
	return nil
}
