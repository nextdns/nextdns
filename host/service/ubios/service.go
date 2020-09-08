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
	"io/ioutil"
	"os"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
	"github.com/nextdns/nextdns/host/service/systemd"
)

type Service struct {
	systemd.Service
}

func New(c service.Config) (Service, error) {
	if st, _ := os.Stat("/data/unifi"); !st.IsDir() {
		return Service{}, service.ErrNotSuported
	}
	return Service{
		Service: systemd.Service{
			Config:           c,
			ConfigFileStorer: service.ConfigFileStorer{File: "/data/" + c.Name + ".conf"},
			Path:             "/etc/systemd/system/" + c.Name + ".service",
		},
	}, nil
}

func (s Service) Install() error {
	if err := ioutil.WriteFile("/data/nextdns", script, 0755); err != nil {
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
ConditionFileIsExecutable={{.Executable}}
After=network.target
Before=nss-lookup.target
Wants=nss-lookup.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
Environment={{.RunModeEnv}}=1
ExecStart={{.Executable}}{{range .Arguments}} {{.}}{{end}}
ExecStartPost=ssh -oStrictHostKeyChecking=no 127.0.0.1 ln -sf /data/nextdns /usr/sbin/nextdns
RestartSec=120
LimitMEMLOCK=infinity

[Install]
WantedBy=multi-user.target
`

var script = []byte(`#!/bin/sh

if [ "$(. /etc/os-release; echo "$ID")" = "ubios" ]; then
	podman exec unifi-os nextdns "$@"
else
	nextdns "$@"
fi
`)
