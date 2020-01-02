// Package systemd implements the systemd init system.

package systemd

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path string
}

func New(c service.Config) (Service, error) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return Service{}, service.ErrNotSuported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/etc/" + c.Name + ".conf"},
		Path:             "/etc/systemd/system/" + c.Name + ".service",
	}, nil
}

func (Service) Type() string {
	return "systemd"
}

func (s Service) Install() error {
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0644, s.Config); err != nil {
		return err
	}
	if err := internal.Run("systemctl", "enable", s.Name+".service"); err != nil {
		return err
	}
	return internal.Run("systemctl", "daemon-reload")
}

func (s Service) Uninstall() error {
	err := internal.Run("systemctl", "disable", s.Name+".service")
	if err != nil {
		return err
	}
	if err := os.Remove(s.Path); err != nil {
		return err
	}
	return nil
}

func (s Service) Status() (service.Status, error) {
	exitCode, out, err := internal.RunOutput("systemctl", "is-active", s.Name)
	if exitCode == 0 && err != nil {
		return service.StatusUnknown, err
	}

	switch {
	case strings.HasPrefix(out, "active"):
		return service.StatusRunning, nil
	case strings.HasPrefix(out, "inactive"):
		return service.StatusStopped, nil
	case strings.HasPrefix(out, "failed"):
		return service.StatusUnknown, errors.New("service in failed state")
	default:
		return service.StatusNotInstalled, nil
	}
}

func (s Service) Start() error {
	return internal.Run("systemctl", "start", s.Name+".service")
}

func (s Service) Stop() error {
	return internal.Run("systemctl", "stop", s.Name+".service")
}

func (s Service) Restart() error {
	return internal.Run("systemctl", "restart", s.Name+".service")
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
RestartSec=120

[Install]
WantedBy=multi-user.target
`
