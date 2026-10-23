package openwrt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

// The journal survives daemon restarts and reboots because the UCI overrides do.
const relayStatePath = "/etc/nextdns-openwrt-relay.json"

// relayDNS directs IPv6 clients to the router's DNS listener in relay mode.
// By default, odhcpd relays upstream DNS advertisements, which can bypass the
// router. For each downstream relay interface, setup replaces the IPv6 entries
// in its UCI DNS list with a reachable local IPv6 address, preserves IPv4 entries,
// and reloads odhcpd so it rewrites relayed RA and DHCPv6 DNS options.
// Original values are journaled before changes so teardown or the next startup
// can restore them after an interruption. Restoration only reverts values that
// still match our override, preserving subsequent administrator edits.
type relayDNS struct {
	needed    func() (bool, error)
	statePath string
	call      func(object, method string, args map[string]any, result any) error
	address   func(iface string) (string, error)
	reload    func() error
}

type relaySection struct {
	Type      string `json:".type"`
	Interface string `json:"interface"`
	Master    string `json:"master"`
	RA        string `json:"ra"`
	DHCPv6    string `json:"dhcpv6"`
	DNS       any    `json:"dns,omitempty"` // nil, string, or []any; preserve the UCI type.
}

func (s relaySection) downstreamRelay() bool {
	return s.Type == "dhcp" && !relayMaster(s.Master) &&
		(s.RA == "relay" || s.RA == "hybrid" || s.DHCPv6 == "relay" || s.DHCPv6 == "hybrid")
}

func relayMaster(value string) bool {
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Most installations do not relay IPv6. Do not require rpcd's UCI object
// there; the UCI CLI is already used by the existing dnsmasq integration.
func relayNeeded() (bool, error) {
	out, err := uci("show", "dhcp")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && (strings.HasSuffix(key, ".ra") || strings.HasSuffix(key, ".dhcpv6")) &&
			(value == "'relay'" || value == "'hybrid'") {
			return true, nil
		}
	}
	return false, nil
}

func (s relaySection) network(name string) string {
	if s.Interface != "" {
		return s.Interface
	}
	return name
}

type relayChange struct {
	Section   string `json:"section"`
	Interface string `json:"interface"`
	Original  any    `json:"original"`
	Applied   any    `json:"applied"`
}

type relayState struct {
	Version int           `json:"version"`
	Changes []relayChange `json:"changes"`
}

func (r *Router) relay() *relayDNS {
	if r.relayConfig != nil {
		return r.relayConfig
	}
	d := &relayDNS{statePath: relayStatePath, call: relayUbus, needed: relayNeeded}
	d.address = d.localAddress
	d.reload = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "/etc/init.d/odhcpd", "reload").CombinedOutput()
		if err != nil {
			return fmt.Errorf("odhcpd reload: %w: %s", err, out)
		}
		return nil
	}
	return d
}

func relayUbus(object, method string, args map[string]any, result any) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ubus", "call", object, method, string(data)).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("ubus %s %s: %w: %s", object, method, err, strings.TrimSpace(string(exit.Stderr)))
		}
		return fmt.Errorf("ubus %s %s: %w", object, method, err)
	}
	if result != nil {
		if err := json.Unmarshal(out, result); err != nil {
			return fmt.Errorf("ubus %s %s: %w", object, method, err)
		}
	}
	return nil
}

func (d *relayDNS) sections() (map[string]relaySection, error) {
	var result struct {
		Values map[string]relaySection `json:"values"`
	}
	err := d.call("uci", "get", map[string]any{"config": "dhcp", "type": "dhcp"}, &result)
	if err != nil {
		return nil, err
	}
	if result.Values == nil {
		return nil, errors.New("missing UCI dhcp values")
	}
	return result.Values, nil
}

func validDNSValue(v any) bool {
	switch v := v.(type) {
	case nil, string:
		return true
	case []any:
		if len(v) == 0 {
			return false
		}
		for _, item := range v {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (d *relayDNS) setDNS(section string, value any) error {
	args := map[string]any{"config": "dhcp", "section": section}
	method := "set"
	if value == nil {
		method = "delete"
		args["option"] = "dns"
	} else {
		args["values"] = map[string]any{"dns": value}
	}
	// rpcd replaces a complete list in one saved UCI operation. In particular,
	// don't append our address while leaving upstream IPv6 resolvers available.
	return d.call("uci", method, args, nil)
}

func (d *relayDNS) commit() error {
	return d.call("uci", "commit", map[string]any{"config": "dhcp"}, nil)
}

func (d *relayDNS) setup() (err error) {
	// Recover an interrupted setup/teardown before taking another snapshot.
	if err := d.restore(); err != nil {
		return err
	}
	if d.needed != nil {
		needed, err := d.needed()
		if err != nil {
			return err
		}
		if !needed {
			return nil
		}
	}
	sections, err := d.sections()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(sections))
	for name := range sections {
		names = append(names, name)
	}
	sort.Strings(names)
	state := relayState{Version: 1}
	for _, name := range names {
		s := sections[name]
		if !s.downstreamRelay() {
			continue
		}
		if !validDNSValue(s.DNS) {
			return fmt.Errorf("dhcp.%s.dns: unsupported UCI value", name)
		}
		ip, err := d.address(s.network(name))
		if err != nil {
			return fmt.Errorf("IPv6 relay DNS on %s: %w", name, err)
		}
		applied := relayAdvertisedDNS(s.DNS, ip)
		if reflect.DeepEqual(s.DNS, applied) {
			continue
		}
		state.Changes = append(state.Changes, relayChange{name, s.network(name), s.DNS, applied})
	}
	if len(state.Changes) == 0 {
		return nil
	}
	// Write before any mutation, so a crash at any subsequent point is recoverable.
	if err := d.save(state); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, d.restore())
		}
	}()
	for _, change := range state.Changes {
		// Check again immediately before writing rather than overwriting an edit made
		// while interfaces were being discovered/probed.
		current, e := d.sections()
		if e != nil {
			return e
		}
		s, ok := current[change.Section]
		if !ok || !s.downstreamRelay() || s.network(change.Section) != change.Interface || !reflect.DeepEqual(s.DNS, change.Original) {
			return fmt.Errorf("dhcp.%s changed during IPv6 relay setup", change.Section)
		}
		if err := d.setDNS(change.Section, change.Applied); err != nil {
			return err
		}
	}
	if err := d.commit(); err != nil {
		return err
	}
	return d.reload()
}

func (d *relayDNS) save(state relayState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(d.statePath), ".nextdns-relay-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err := os.Rename(f.Name(), d.statePath); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(d.statePath))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (d *relayDNS) restore() error {
	data, err := os.ReadFile(d.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state relayState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("read IPv6 relay recovery state: %w", err)
	}
	if state.Version != 1 {
		return fmt.Errorf("unsupported IPv6 relay recovery version %d", state.Version)
	}
	for _, c := range state.Changes {
		if c.Section == "" || c.Interface == "" || !validDNSValue(c.Original) || !validDNSValue(c.Applied) || c.Applied == nil {
			return errors.New("invalid IPv6 relay recovery state")
		}
	}
	for _, c := range state.Changes {
		sections, err := d.sections()
		if err != nil {
			return err
		}
		s, ok := sections[c.Section]
		// A removed/replaced section or an administrator's DNS edit is not ours to restore.
		if !ok || s.Type != "dhcp" || s.network(c.Section) != c.Interface || !reflect.DeepEqual(s.DNS, c.Applied) {
			continue
		}
		if err := d.setDNS(c.Section, c.Original); err != nil {
			return err
		}
	}
	// Always commit/reload on retry, even if a preceding attempt already restored
	// the option but failed at commit or reload. Retain the journal until both succeed.
	if err := d.commit(); err != nil {
		return err
	}
	if err := d.reload(); err != nil {
		return err
	}
	return os.Remove(d.statePath)
}
