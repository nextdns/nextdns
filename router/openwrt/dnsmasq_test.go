package openwrt

import (
	"fmt"
	"os"
	"testing"
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
