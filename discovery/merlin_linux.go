package discovery

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Merlin struct {
	mu sync.RWMutex
	m  map[string]string
}

func (r *Merlin) Start(ctx context.Context) error {
	b, err := exec.Command("uname", "-o").Output()
	if err != nil || !strings.HasPrefix(string(b), "ASUSWRT-Merlin") {
		return nil
	}

	t := TraceFromCtx(ctx)
	if err := r.clientList(ctx); err != nil && t.OnWarning != nil {
		t.OnWarning(fmt.Sprintf("clientList: %v", err))
	}
	go func() {
		for {
			select {
			case <-time.After(120 * time.Second):
				if err := r.clientList(ctx); err != nil && t.OnWarning != nil {
					t.OnWarning(fmt.Sprintf("clientList: %v", err))
				}
			case <-ctx.Done():
				break
			}
		}
	}()
	return nil
}

func (r *Merlin) Lookup(addr string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	name, found := r.m[addr]
	return name, found
}

func (r *Merlin) clientList(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "nvram", "get", "custom_clientlist")
	b, err := cmd.Output()
	if err != nil {
		return err
	}
	entries, err := readClientList(b)
	if err != nil {
		return err
	}
	t := TraceFromCtx(ctx)
	for addr, name := range entries {
		r.mu.Lock()
		if r.m[addr] != name {
			if r.m == nil {
				r.m = map[string]string{}
			}
			r.m[addr] = name
			r.mu.Unlock()
			if t.OnDiscover != nil {
				t.OnDiscover(addr, name, "Merlin")
			}
		} else {
			r.mu.Unlock()
		}
	}
	return nil
}

func readClientList(b []byte) (map[string]string, error) {
	if len(b) == 0 {
		return nil, nil
	}
	m := map[string]string{}
	for len(b) > 0 {
		if len(b) < 1 || b[0] != '<' {
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
		name := normalizeName(string(b[:idx]))
		addr := string(bytes.ToLower(b[idx+1 : idx2]))
		if name != "" {
			m[addr] = name
		}
		b = b[eol:]
	}
	return m, nil
}
