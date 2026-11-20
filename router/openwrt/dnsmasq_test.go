package openwrt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func Test_parseUbusDnsmasqConfDir(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"23-05", "/tmp/dnsmasq.d"},
		{"24-10", "/tmp/dnsmasq.cfg01411c.d"},
		{"empty", "/tmp/dnsmasq.d"},
		{"invalid", "/tmp/dnsmasq.d"},
		{"missing-instance", "/tmp/dnsmasq.d"},
		{"missing-mount", "/tmp/dnsmasq.d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := os.ReadFile(fmt.Sprintf("testdata/ubus-%s.json", tt.name))
			if err != nil {
				t.Fatal(err)
			}
			if got := parseUbusDnsmasqConfDir(out); got != tt.want {
				t.Errorf("dnsmaskConfDir() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseUbusDNSMasqDirRequiresRegisteredInstance(t *testing.T) {
	for _, name := range []string{"empty", "invalid", "missing-instance", "missing-mount"} {
		out, err := os.ReadFile("testdata/ubus-"+name+".json")
		if err != nil {
			t.Fatal(err)
		}
		if dir, ok := parseUbusDNSMasqDir(out); ok {
			t.Errorf("%s: unexpected directory %q", name, dir)
		}
	}
}

func Test_waitForDNSMasqConfDir(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	path, err := waitForDNSMasqConfDir(time.Second, func() (string, bool) {
		calls++
		if calls == 1 {
			return "/tmp/dnsmasq.d", false
		}
		return dir, true
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "nextdns.conf") {
		t.Fatalf("unexpected path %q", path)
	}
}

func Test_waitForDNSMasqConfDirTimeout(t *testing.T) {
	_, err := waitForDNSMasqConfDir(0, func() (string, bool) { return "", false })
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("unexpected error: %v", err)
	}
}
