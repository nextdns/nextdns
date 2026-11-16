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
