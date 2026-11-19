package ubios

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

func renderUnit(t *testing.T, c service.Config) string {
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
	return strings.ReplaceAll(string(b), exe, "/data/nextdns")
}

func TestUnitTemplate(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
		want  string
	}{
		{
			name: "default",
			want: `[Unit]
Description=NextDNS DNS53 to DoH proxy.
Wants=nss-lookup.target network-online.target
After=nss-lookup.target network-online.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
Environment=SERVICE_RUN_MODE=1
ExecStartPre=-/bin/cp -f /data/nextdns /data/.nextdns-recover
ExecStartPre=-/bin/cp -f /data/.nextdns-recover /data/nextdns
ExecStart=/data/nextdns run -config-file /data/nextdns.conf
Restart=on-failure
RestartSec=120
LimitMEMLOCK=infinity

[Install]
WantedBy=multi-user.target
`,
		},
		{
			name:  "podman",
			flags: []string{"podman"},
			want: `[Unit]
Description=NextDNS DNS53 to DoH proxy.
Wants=nss-lookup.target network-online.target
After=nss-lookup.target network-online.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
Environment=SERVICE_RUN_MODE=1
ExecStart=/data/nextdns run -config-file /data/nextdns.conf
ExecStartPost=ssh -oStrictHostKeyChecking=no 127.0.0.1 ln -sf /data/nextdns /usr/bin/nextdns
Restart=on-failure
RestartSec=120
LimitMEMLOCK=infinity

[Install]
WantedBy=multi-user.target
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderUnit(t, service.Config{
				Name:        "nextdns",
				Description: "NextDNS DNS53 to DoH proxy.",
				Arguments:   []string{"run", "-config-file", "/data/nextdns.conf"},
				Flags:       tt.flags,
			})
			if got != tt.want {
				t.Errorf("unexpected unit:\n--- got ---\n%s\n--- want ---\n%s", got, tt.want)
			}
		})
	}
}
