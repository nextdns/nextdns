package systemd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

// renderUnit renders tmpl the same way Install does and returns the result
// with the test binary path replaced by a fixed placeholder.
func renderUnit(t *testing.T, tmpl string, c service.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), c.Name+".service")
	if err := internal.CreateWithTemplate(path, tmpl, 0644, c); err != nil {
		t.Fatalf("render unit: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), exe, "/usr/bin/nextdns")
}

func TestUnitTemplate(t *testing.T) {
	got := renderUnit(t, tmpl, service.Config{
		Name:        "nextdns",
		Description: "NextDNS DNS53 to DoH proxy.",
		Arguments:   []string{"run", "-config-file", "/etc/nextdns.conf"},
	})
	want := `[Unit]
Description=NextDNS DNS53 to DoH proxy.
ConditionFileIsExecutable=/usr/bin/nextdns
After=network.target
Before=nss-lookup.target
Wants=nss-lookup.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
Environment=SERVICE_RUN_MODE=1
ExecStart=/usr/bin/nextdns run -config-file /etc/nextdns.conf
Restart=on-failure
RestartSec=120
LimitMEMLOCK=infinity

[Install]
WantedBy=multi-user.target
`
	if got != want {
		t.Errorf("unexpected unit:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
