// Package ubios implements the Unifi OS init system.
//
// On ubios the base system does not provide a hook to start custom services.
// Although, the base system starts a podman (Docker like) container with a
// standard Debian stretch with systemd.
//
// The trick is thus to install a systemd init that will start the service from
// inside the container.

package ubios

import (
	"bufio"
	"os"
	"os/exec"
	"strings"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
	"github.com/nextdns/nextdns/host/service/systemd"
)

type Service struct {
	systemd.Service
}

func isUnifi() bool {
	if st, _ := os.Stat("/data/unifi"); st != nil && st.IsDir() {
		return true
	}
	if err := exec.Command("ubnt-device-info", "firmware").Run(); err == nil {
		return true
	}
	return false
}

func New(c service.Config) (Service, error) {
	if !isUnifi() {
		return Service{}, service.ErrNotSupported
	}
	srv := Service{
		Service: systemd.Service{
			Config:           c,
			ConfigFileStorer: service.ConfigFileStorer{File: "/data/" + c.Name + ".conf"},
			Path:             "/etc/systemd/system/" + c.Name + ".service",
		},
	}
	if usePodman, _ := isContainerized(); usePodman {
		srv.Config.Flags = append(srv.Config.Flags, "podman")
	}
	return srv, nil
}

func isContainerized() (bool, error) {
	f, err := os.Open("/proc/1/cgroup")
	if err != nil {
		return false, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		flds := strings.Split(s.Text(), ":")
		if len(flds) != 3 {
			continue
		}
		if flds[2] != "/" && flds[2] != "/init.scope" {
			return true, nil
		}
	}
	return false, nil
}

func (s Service) Install() error {
	if err := os.WriteFile("/data/nextdns", script, 0755); err != nil {
		return err
	}
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0644, s.Config); err != nil {
		return err
	}
	if err := internal.Run("systemctl", "enable", s.Name+".service"); err != nil {
		return err
	}
	return internal.Run("systemctl", "daemon-reload")
}

func (s Service) Uninstall() error {
	os.Remove("/data/nextdns")
	return s.Service.Uninstall()
}

var tmpl = `[Unit]
Description={{.Description}}
Before=nss-lookup.target
Wants=nss-lookup.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
Environment={{.RunModeEnv}}=1
{{- if not (.Config.HasFlag "podman") }}
ExecStartPre=-/bin/cp -f {{.Executable}} /data/.nextdns-recover
ExecStartPre=-/bin/cp -f /data/.nextdns-recover {{.Executable}}
{{- end}}
ExecStart={{.Executable}}{{range .Arguments}} {{.}}{{end}}
{{- if (.Config.HasFlag "podman") }}
ExecStartPost=ssh -oStrictHostKeyChecking=no 127.0.0.1 ln -sf /data/nextdns /usr/bin/nextdns
{{- end}}
RestartSec=120
LimitMEMLOCK=infinity

[Install]
WantedBy=multi-user.target
`

var script = []byte(`#!/bin/sh

if [ "$(. /etc/os-release; echo "$ID")" = "ubios" ]; then
	if [ "$1" = "upgrade" ]; then
		RUN_COMMAND=upgrade sh -c "$(curl -s https://nextdns.io/install)"
	fi
	podman exec unifi-os nextdns "$@"
else
	nextdns "$@"
fi
`)
