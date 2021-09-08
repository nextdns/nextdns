// Package upstart implements the Upstart init system.

package upstart

import (
	"fmt"
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
	if _, err := exec.LookPath("initctl"); err != nil {
		return Service{}, service.ErrNotSupported
	}
	out, err := internal.RunOutput("initctl", "version")
	if err != nil || !strings.Contains(out, "upstart") {
		return Service{}, service.ErrNotSupported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/etc/" + c.Name + ".conf"},
		Path:             "/etc/init/" + c.Name + ".conf",
	}, nil
}

func (s Service) Install() error {
	return internal.CreateWithTemplate(s.Path, tmpl, 0644, s.Config)
}

func (s Service) Uninstall() error {
	if err := os.Remove(s.Path); err != nil {
		return err
	}
	return nil
}

func (s Service) Status() (service.Status, error) {
	out, err := internal.RunOutput("initctl", "status", s.Name)
	if internal.ExitCode(err) == 0 && err != nil {
		return service.StatusUnknown, err
	}

	switch {
	case strings.HasPrefix(out, fmt.Sprintf("%s start/running", s.Name)):
		return service.StatusRunning, nil
	case strings.HasPrefix(out, fmt.Sprintf("%s stop/waiting", s.Name)):
		return service.StatusStopped, nil
	default:
		return service.StatusNotInstalled, nil
	}
}

func (s Service) Start() error {
	return internal.Run("initctl", "start", s.Name)
}

func (s Service) Stop() error {
	return internal.Run("initctl", "stop", s.Name)
}

func (s Service) Restart() error {
	return internal.Run("initctl", "restart", s.Name)
}

var tmpl = `# {{.Description}}

{{if .DisplayName}}description "{{.DisplayName}}"{{end}}
start on filesystem or runlevel [2345]
stop on runlevel [!2345]

respawn
respawn limit 10 5
umask 022

console none

env {{.RunModeEnv}}=1

script	
	exec {{.Executable}}{{range .Arguments}} {{.}}{{end}}
end script
`
