package ubios

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextdns/nextdns/router/internal"
)

func TestDNSMasqTemplateCapsEDNS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nextdns.conf")
	r := &Router{DNSMasqConfPath: path, ListenPort: "5342", ClientReporting: true}
	if err := internal.WriteTemplate(path, tmpl, r, 0644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"server=127.0.0.1#5342\n",
		"add-mac\n",
		"add-subnet=32,128\n",
		"edns-packet-max=1232\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated config missing %q:\n%s", want, got)
		}
	}
}
