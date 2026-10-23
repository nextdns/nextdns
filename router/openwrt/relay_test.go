package openwrt

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type relayFixture struct {
	d       *relayDNS
	values  map[string]relaySection
	disk    map[string]relaySection
	calls   []string
	fail    string
	after   bool
	reloads int
}

func cloneSections(in map[string]relaySection) map[string]relaySection {
	b, _ := json.Marshal(in)
	var out map[string]relaySection
	_ = json.Unmarshal(b, &out)
	return out
}

func newRelayFixture(t *testing.T, values map[string]relaySection) *relayFixture {
	t.Helper()
	f := &relayFixture{values: cloneSections(values), disk: cloneSections(values)}
	f.d = &relayDNS{
		statePath: filepath.Join(t.TempDir(), "recovery.json"),
		call:      f.call,
		address:   func(iface string) (string, error) { return "fd00::1", nil },
		reload: func() error {
			f.calls = append(f.calls, "reload")
			if f.fail == "reload" {
				f.fail = ""
				return errors.New("reload failed")
			}
			f.reloads++
			return nil
		},
	}
	return f
}

func (f *relayFixture) call(object, method string, args map[string]any, result any) error {
	if object != "uci" || args["config"] != "dhcp" {
		return fmt.Errorf("unexpected call: %s %s %v", object, method, args)
	}
	section, _ := args["section"].(string)
	op := method
	if section != "" {
		op += " " + section
	}
	f.calls = append(f.calls, op)
	fail := f.fail == op
	if fail {
		f.fail = ""
		if !f.after {
			return errors.New("injected " + op)
		}
	}
	switch method {
	case "get":
		values := cloneSections(f.values)
		if typ, ok := args["type"].(string); ok {
			for name, s := range values {
				if s.Type != typ {
					delete(values, name)
				}
			}
		}
		b, _ := json.Marshal(map[string]any{"values": values})
		if err := json.Unmarshal(b, result); err != nil {
			return err
		}
	case "set":
		s, ok := f.values[section]
		if !ok {
			return errors.New("section not found")
		}
		values := args["values"].(map[string]any)
		if len(values) != 1 {
			return errors.New("changed unrelated options")
		}
		s.DNS = values["dns"]
		f.values[section] = s
	case "delete":
		if args["option"] != "dns" {
			return errors.New("deleted unrelated option")
		}
		s := f.values[section]
		s.DNS = nil
		f.values[section] = s
	case "commit":
		f.disk = cloneSections(f.values)
	default:
		return fmt.Errorf("unexpected method %q", method)
	}
	if fail {
		return errors.New("injected uncertain " + op)
	}
	return nil
}

func relayLAN(dns any) relaySection {
	return relaySection{Type: "dhcp", Interface: "lan", RA: "relay", DHCPv6: "relay", DNS: dns}
}

func assertNoJournal(t *testing.T, d *relayDNS) {
	t.Helper()
	if _, err := os.Stat(d.statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still present: %v", err)
	}
}

func TestRelaySetupRestore(t *testing.T) {
	for _, original := range []any{nil, "", "2001:db8::53", []any{"2001:db8::53", "2001:db8::54"}, []any{"192.168.1.1", "2001:db8::53"}} {
		t.Run(fmt.Sprint(original), func(t *testing.T) {
			values := map[string]relaySection{
				"lan":      relayLAN(original),
				"guest":    {Type: "dhcp", Interface: "guest", DHCPv6: "hybrid"},
				"upstream": {Type: "dhcp", Master: "1", RA: "relay"},
				"server":   {Type: "dhcp", Interface: "other", RA: "server"},
				"disabled": {Type: "dhcp", RA: "disabled"},
				"not_dhcp": {Type: "dnsmasq", RA: "relay"},
			}
			f := newRelayFixture(t, values)
			if err := f.d.setup(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.values["lan"].DNS, relayAdvertisedDNS(original, "fd00::1")) {
				t.Fatalf("wrong override: %#v", f.values["lan"].DNS)
			}
			if !reflect.DeepEqual(f.values["guest"].DNS, []any{"fd00::1"}) {
				t.Fatal("guest not overridden")
			}
			for _, name := range []string{"upstream", "server", "disabled", "not_dhcp"} {
				if !reflect.DeepEqual(f.values[name], values[name]) {
					t.Fatalf("modified %s", name)
				}
			}
			if info, err := os.Stat(f.d.statePath); err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("journal permissions: %v %v", info, err)
			}
			// Restore through a fresh manager, as after a process restart/reboot.
			recovered := *f.d
			f.values = cloneSections(f.disk)
			if err := recovered.restore(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.disk, values) {
				t.Fatalf("restore mismatch: %#v != %#v", f.disk, values)
			}
			assertNoJournal(t, f.d)
			calls := len(f.calls)
			if err := recovered.restore(); err != nil {
				t.Fatal(err)
			}
			if len(f.calls) != calls {
				t.Fatal("second restore performed work")
			}
		})
	}
}

func TestRelaySetupRollback(t *testing.T) {
	for _, fail := range []string{"set lan", "set other", "commit", "reload"} {
		for _, after := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/after=%v", fail, after), func(t *testing.T) {
				values := map[string]relaySection{"lan": relayLAN(nil), "other": relayLAN([]any{"2001:db8::53"})}
				f := newRelayFixture(t, values)
				f.fail = fail
				f.after = after
				if err := f.d.setup(); err == nil {
					t.Fatal("expected setup failure")
				}
				if !reflect.DeepEqual(f.disk, values) {
					t.Fatalf("rollback failed: %#v", f.disk)
				}
				assertNoJournal(t, f.d)
			})
		}
	}
}

func TestRelayRestoreRetry(t *testing.T) {
	for _, fail := range []string{"delete lan", "set other", "commit", "reload"} {
		t.Run(fail, func(t *testing.T) {
			values := map[string]relaySection{"lan": relayLAN(nil), "other": relayLAN("2001:db8::53")}
			f := newRelayFixture(t, values)
			if err := f.d.setup(); err != nil {
				t.Fatal(err)
			}
			f.fail = fail
			if err := f.d.restore(); err == nil {
				t.Fatal("expected restore error")
			}
			if _, err := os.Stat(f.d.statePath); err != nil {
				t.Fatal("lost recovery record", err)
			}
			// Do not reset staged UCI state; a new process sees saved deltas too.
			recovered := *f.d
			if err := recovered.restore(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.disk, values) {
				t.Fatalf("retry failed: %#v", f.disk)
			}
			assertNoJournal(t, f.d)
		})
	}
}

func TestRelayPreservesAdministratorChanges(t *testing.T) {
	for _, change := range []string{"dns", "remove dns", "remove section", "replace section", "unrelated"} {
		t.Run(change, func(t *testing.T) {
			f := newRelayFixture(t, map[string]relaySection{"lan": relayLAN("2001:db8::53")})
			if err := f.d.setup(); err != nil {
				t.Fatal(err)
			}
			s := f.values["lan"]
			switch change {
			case "dns":
				s.DNS = []any{"fd00::99"}
			case "remove dns":
				s.DNS = nil
			case "replace section":
				s.Interface = "replacement"
			case "unrelated":
				s.RA = "server"
			}
			f.values["lan"] = s
			if change == "remove section" {
				delete(f.values, "lan")
			}
			want := cloneSections(f.values)
			if change == "unrelated" {
				s.DNS = "2001:db8::53"
				want["lan"] = s
			}
			if err := f.d.restore(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(f.values, want) {
				t.Fatalf("overwrote admin changes: %#v != %#v", f.values, want)
			}
		})
	}
}

func TestRelayRestartKeepsOriginalSnapshot(t *testing.T) {
	f := newRelayFixture(t, map[string]relaySection{"lan": relayLAN(nil)})
	for i := 0; i < 3; i++ {
		recovered := *f.d
		if err := recovered.setup(); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.d.restore(); err != nil {
		t.Fatal(err)
	}
	if f.values["lan"].DNS != nil {
		t.Fatal("saved override instead of original")
	}
}

func TestRelayPreflightFailures(t *testing.T) {
	for _, failure := range []string{"address", "journal", "invalid dns", "get", "malformed journal", "unknown version"} {
		t.Run(failure, func(t *testing.T) {
			f := newRelayFixture(t, map[string]relaySection{"lan": relayLAN(nil)})
			switch failure {
			case "address":
				f.d.address = func(string) (string, error) { return "", errors.New("no local listener") }
			case "journal":
				f.d.statePath = filepath.Join(t.TempDir(), "missing", "state")
			case "invalid dns":
				s := f.values["lan"]
				s.DNS = 42
				f.values["lan"] = s
			case "get":
				f.fail = "get"
			case "malformed journal":
				_ = os.WriteFile(f.d.statePath, []byte("{"), 0600)
			case "unknown version":
				_ = os.WriteFile(f.d.statePath, []byte(`{"version":99}`), 0600)
			}
			if err := f.d.setup(); err == nil {
				t.Fatal("expected error")
			}
			for _, op := range f.calls {
				if op != "get" {
					t.Fatalf("mutation during preflight: %s", op)
				}
			}
		})
	}
}

func TestRelayConcurrentEditRollsBackEarlierChanges(t *testing.T) {
	f := newRelayFixture(t, map[string]relaySection{"lan": relayLAN(nil), "other": relayLAN(nil)})
	call := f.d.call
	f.d.call = func(object, method string, args map[string]any, result any) error {
		err := call(object, method, args, result)
		if method == "set" && args["section"] == "lan" {
			s := f.values["other"]
			s.DNS = "fd00::99"
			f.values["other"] = s
		}
		return err
	}
	if err := f.d.setup(); err == nil || !strings.Contains(err.Error(), "changed during") {
		t.Fatalf("got %v", err)
	}
	if f.values["lan"].DNS != nil || f.values["other"].DNS != "fd00::99" {
		t.Fatalf("bad rollback: %#v", f.values)
	}
}

func TestRelayNoop(t *testing.T) {
	for _, s := range []relaySection{{Type: "dhcp", RA: "server"}, relayLAN([]any{"fd00::1"})} {
		f := newRelayFixture(t, map[string]relaySection{"lan": s})
		if err := f.d.setup(); err != nil {
			t.Fatal(err)
		}
		if f.reloads != 0 {
			t.Fatal("unnecessary reload")
		}
		assertNoJournal(t, f.d)
	}
}

func TestRouterCachedSetupIncludesRelay(t *testing.T) {
	f := newRelayFixture(t, map[string]relaySection{"lan": relayLAN(nil)})
	r := Router{CacheEnabled: true, relayConfig: f.d}
	if err := r.Setup(); err != nil {
		t.Fatal(err)
	}
	if f.reloads != 1 {
		t.Fatal("cache mode skipped relay setup")
	}
}

func fakeLegacyUCI(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("UCI shell fixture requires Unix")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1 $2" in
 'get network.lan.ipaddr') echo 192.168.1.1 ;;
 'get dhcp.@dnsmasq[0].server') echo 'uci: Entry not found' >&2; exit 1 ;;
 'get dhcp.lan.dhcp_option') echo '6,192.168.1.1' ;;
 'get dhcp.@dnsmasq[0].port') echo 'uci: Entry not found' >&2; exit 1 ;;
 'del_list dhcp.lan.dhcp_option=6,192.168.1.1'|'commit ') ;;
 *) echo "unexpected uci: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "uci"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRouterRelayLifecycle(t *testing.T) {
	for _, cache := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache=%v", cache), func(t *testing.T) {
			fakeLegacyUCI(t)
			f := newRelayFixture(t, map[string]relaySection{"lan": relayLAN(nil)})
			r := Router{CacheEnabled: cache, ListenPort: "5342", DNSMasqPath: filepath.Join(t.TempDir(), "nextdns.conf"), relayConfig: f.d}
			restarts := 0
			r.dnsmasqRestart = func() error { restarts++; return nil }
			if cache {
				if err := r.setupDNSMasq(); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Setup(); err != nil {
				t.Fatal(err)
			}
			if restarts != 1 || f.reloads != 1 {
				t.Fatalf("restarts=%d reloads=%d", restarts, f.reloads)
			}
			if err := r.Restore(); err != nil {
				t.Fatal(err)
			}
			if f.values["lan"].DNS != nil || restarts != 2 {
				t.Fatal("restore did not restore both services")
			}
			assertNoJournal(t, f.d)
		})
	}
}

func TestRouterRestoreRelayDespiteDNSMasqFailure(t *testing.T) {
	fakeLegacyUCI(t)
	f := newRelayFixture(t, map[string]relaySection{"lan": relayLAN(nil)})
	if err := f.d.setup(); err != nil {
		t.Fatal(err)
	}
	r := Router{relayConfig: f.d, dnsmasqRestart: func() error { return errors.New("dnsmasq failed") }}
	if err := r.Restore(); err == nil {
		t.Fatal("expected dnsmasq error")
	}
	if f.values["lan"].DNS != nil {
		t.Fatal("relay restoration skipped")
	}
	assertNoJournal(t, f.d)
}

func TestRelayMissingRPCIsIrrelevantWithoutRelay(t *testing.T) {
	f := newRelayFixture(t, map[string]relaySection{})
	f.d.needed = func() (bool, error) { return false, nil }
	f.fail = "get"
	if err := f.d.setup(); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatal("called rpcd unnecessarily")
	}
}
