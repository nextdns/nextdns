//go:build unix

package openwrt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupDNSMasqExplicitPort53(t *testing.T) {
	d := t.TempDir()
	script := `#!/bin/sh
case "$*" in
"get dhcp.@dnsmasq[0].port") echo 53;;
"get network.lan.ipaddr") echo 192.168.1.1;;
"get dhcp.lan.dhcp_option") echo 6,192.168.1.1;;
esac
`
	if err := os.WriteFile(filepath.Join(d, "uci"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", d+":"+os.Getenv("PATH"))
	r := &Router{DNSMasqPath: filepath.Join(d, "nextdns.conf"), CacheEnabled: true, dnsmasqRestart: func() error { return nil }}
	if err := r.setupDNSMasq(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(r.DNSMasqPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "port=0") {
		t.Fatalf("explicit port=53 removed but dnsmasq still binds default 53: generated config %q", b)
	}
}

// TestSetupDNSMasqBeforeAndAfterDnsmasqStarts covers being started before
// dnsmasq, on the first boot after a sysupgrade: the config is written where
// dnsmasq will load it and dnsmasq is not restarted, unless it has been started
// in the meantime and can have missed the config.
func TestSetupDNSMasqBeforeAndAfterDnsmasqStarts(t *testing.T) {
	tests := []struct {
		name string
		// startedAt is the lookup of the generated config from which dnsmasq
		// is started: the first one is for its conf-dir, the second one
		// follows the write of our config.
		startedAt    int
		wantRestarts int
	}{
		{"before", 3, 0},
		{"during", 2, 1},
		{"after", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := t.TempDir()
			script := `#!/bin/sh
case "$*" in
"get network.lan.ipaddr") echo 192.168.1.1;;
"get dhcp.lan.dhcp_option") echo 6,192.168.1.1;;
*) echo "uci: Entry not found" >&2; exit 1;;
esac
`
			if err := os.WriteFile(filepath.Join(d, "uci"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", d+":"+os.Getenv("PATH"))

			confDir := filepath.Join(d, "dnsmasq.cfg01411c.d")
			env := systemDnsmasqEnv
			t.Cleanup(func() { systemDnsmasqEnv = env })
			lookups := 0
			systemDnsmasqEnv = dnsmasqEnv{
				uci: func(args ...string) (string, error) {
					if strings.Join(args, " ") == "-X show dhcp.@dnsmasq[0]" {
						return "dhcp.cfg01411c=dnsmasq", nil
					}
					return confDir, nil // dhcp.@dnsmasq[0].confdir
				},
				readFile: func(name string) ([]byte, error) {
					return []byte("conf-dir=" + confDir + "\n"), nil
				},
				glob: func(string) ([]string, error) {
					if lookups++; lookups >= tt.startedAt {
						return []string{"/var/etc/dnsmasq.conf.cfg01411c"}, nil
					}
					return nil, nil
				},
			}
			if tt.startedAt == 1 {
				// The init script created the conf-dir when it started dnsmasq.
				if err := os.Mkdir(confDir, 0755); err != nil {
					t.Fatal(err)
				}
			}

			restarts := 0
			r := &Router{CacheEnabled: true, dnsmasqRestart: func() error { restarts++; return nil }}
			if err := r.setupDNSMasq(); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(confDir, "nextdns.conf"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), "port=0") {
				t.Errorf("generated config %q, want port=0", b)
			}
			if restarts != tt.wantRestarts {
				t.Errorf("dnsmasq restarts = %d, want %d", restarts, tt.wantRestarts)
			}
		})
	}
}
