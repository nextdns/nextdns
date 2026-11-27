package synology

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/nextdns/nextdns/config"
)

func TestHasPortZero(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{name: "disabled", config: "port=0\n", want: true},
		{name: "whitespace", config: "  port=0  \n", want: true},
		{name: "spaces around equals", config: "port = 0\n", want: false},
		{name: "commented", config: "# port=0\n", want: false},
		{name: "different port", config: "port=53\n", want: false},
		{name: "different option", config: "server=127.0.0.1#53\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPortZero([]byte(tt.config)); got != tt.want {
				t.Fatalf("hasPortZero(%q) = %v, want %v", tt.config, got, tt.want)
			}
		})
	}
}

func TestTemplateDoesNotRepeatPort(t *testing.T) {
	tests := []struct {
		name           string
		portZeroExists bool
		wantPort       bool
	}{
		{name: "not present", wantPort: true},
		{name: "already present", portZeroExists: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Router{CacheEnabled: true, PortZeroExists: tt.portZeroExists}
			var b bytes.Buffer
			if err := template.Must(template.New("").Parse(tmpl)).Execute(&b, r); err != nil {
				t.Fatal(err)
			}

			if gotPort := strings.Contains(b.String(), "port=0"); gotPort != tt.wantPort {
				t.Fatalf("template contains port=0 = %v, want %v; output: %q", gotPort, tt.wantPort, b.String())
			}
		})
	}
}

type fakeSystem struct {
	dir      string
	restarts int
}

func newTestRouter(t *testing.T, dhcpEnabled, port53Free, dhcpDNSMasq bool) (*Router, *fakeSystem) {
	t.Helper()
	fs := &fakeSystem{dir: t.TempDir()}
	if dhcpEnabled {
		if err := os.WriteFile(filepath.Join(fs.dir, "dhcpd.info"), []byte(`enable="yes"`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	r := &Router{
		DNSMasqPath:        filepath.Join(fs.dir, "dhcpd-vendor-nextdns.conf"),
		ListenPort:         "5342",
		dhcpdDir:           fs.dir,
		port53Free:         func() bool { return port53Free },
		dhcpDNSMasqRunning: func() bool { return dhcpDNSMasq },
		restartDNSMasq:     func() error { fs.restarts++; return nil },
	}
	return r, fs
}

func TestConfigureSetupRestore(t *testing.T) {
	const forwarder = "server=127.0.0.1#5342"
	tests := []struct {
		name        string
		dhcpEnabled bool
		port53Free  bool
		dhcpDNSMasq bool
		cache       bool
		wantListen  string
		wantErr     bool
		wantConf    string // expected in DNSMasqPath after Setup, "" for none
	}{
		{name: "dhcp disabled, port 53 free", port53Free: true, wantListen: ":53"},
		{name: "dhcp disabled, port 53 busy, dhcp dnsmasq", dhcpDNSMasq: true, wantListen: "127.0.0.1:5342", wantConf: forwarder},
		{name: "dhcp disabled, port 53 busy, dhcp dnsmasq, cache", dhcpDNSMasq: true, cache: true, wantListen: ":53", wantConf: "port=0"},
		{name: "dhcp disabled, port 53 busy, other process", wantListen: ":53", wantErr: true},
		{name: "dhcp enabled, port 53 busy", dhcpEnabled: true, dhcpDNSMasq: true, wantListen: "127.0.0.1:5342", wantConf: forwarder},
		{name: "dhcp enabled, port 53 busy, cache", dhcpEnabled: true, dhcpDNSMasq: true, cache: true, wantListen: ":53", wantConf: "port=0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, fs := newTestRouter(t, tt.dhcpEnabled, tt.port53Free, tt.dhcpDNSMasq)
			c := &config.Config{CacheSize: "0"}
			if tt.cache {
				c.CacheSize = "10MB"
			}
			err := r.Configure(c)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Configure() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(c.Listens) != 1 || c.Listens[0] != tt.wantListen {
				t.Fatalf("Listens = %v, want [%s]", c.Listens, tt.wantListen)
			}
			if err := r.Setup(); err != nil {
				t.Fatalf("Setup() error = %v", err)
			}

			b, err := os.ReadFile(r.DNSMasqPath)
			if tt.wantConf == "" {
				if err == nil {
					t.Fatalf("unexpected dnsmasq config: %q", b)
				}
				if fs.restarts != 0 {
					t.Fatalf("dnsmasq restarted %d times, want 0", fs.restarts)
				}
			} else {
				if !strings.Contains(string(b), tt.wantConf) {
					t.Fatalf("dnsmasq config %q does not contain %q", b, tt.wantConf)
				}
				info, err := os.ReadFile(filepath.Join(fs.dir, "dhcpd-vendor-nextdns.info"))
				if err != nil || string(info) != `enable="yes"` {
					t.Fatalf("dnsmasq config not enabled: %q, %v", info, err)
				}
				if fs.restarts != 1 {
					t.Fatalf("dnsmasq restarted %d times, want 1", fs.restarts)
				}
			}
			// A loopback listener is only reachable by clients through the
			// dnsmasq forwarder.
			if strings.HasPrefix(c.Listens[0], "127.0.0.1:") && !strings.Contains(string(b), forwarder) {
				t.Fatalf("listening on %s without a dnsmasq forwarder", c.Listens[0])
			}

			restarts := fs.restarts
			if err := r.Restore(); err != nil {
				t.Fatalf("Restore() error = %v", err)
			}
			if _, err := os.Stat(r.DNSMasqPath); !os.IsNotExist(err) {
				t.Fatalf("dnsmasq config still present after Restore: %v", err)
			}
			wantRestarts := restarts
			if tt.wantConf != "" {
				wantRestarts++
			}
			if fs.restarts != wantRestarts {
				t.Fatalf("Restore restarted dnsmasq %d times, want %d", fs.restarts-restarts, wantRestarts-restarts)
			}
		})
	}
}
