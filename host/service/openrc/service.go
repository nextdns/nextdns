// Package openrc implements the OpenRC init system.

package openrc

import (
	"os"
	"os/exec"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path string
}

func New(c service.Config) (Service, error) {
	if b, err := os.ReadFile("/proc/1/comm"); err != nil || string(b) != "init\n" {
		return Service{}, service.ErrNotSupported
	}
	if _, err := os.Stat("/sbin/openrc-run"); err != nil {
		return Service{}, service.ErrNotSupported
	}
	if b, err := exec.Command("rc-status", "--runlevel").Output(); err != nil || string(b) != "default\n" {
		return Service{}, service.ErrNotSupported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/etc/" + c.Name + ".conf"},
		Path:             "/etc/init.d/" + c.Name,
	}, nil
}

func (s Service) Install() error {
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config); err != nil {
		return err
	}

	if err := internal.Run("rc-update", "add", s.Name); err != nil {
		return err
	}

	return nil
}

func (s Service) Uninstall() error {
	if err := internal.Run("rc-update", "del", s.Name); err != nil {
		return err
	}

	if err := os.Remove(s.Path); err != nil {
		return err
	}

	return nil
}

func (s Service) Status() (service.Status, error) {
	err := internal.Run("rc-service", "-q", s.Name, "status")
	if internal.ExitCode(err) == 1 {
		return service.StatusNotInstalled, nil
	} else if internal.ExitCode(err) == 3 {
		return service.StatusStopped, nil
	} else if err != nil {
		return service.StatusUnknown, err
	}
	return service.StatusRunning, nil
}

func (s Service) Start() error {
	return internal.Run("rc-service", s.Name, "start")
}

func (s Service) Stop() error {
	return internal.Run("rc-service", s.Name, "stop")
}

func (s Service) Restart() error {
	return internal.Run("rc-service", s.Name, "restart")
}

var tmpl = `#!/sbin/openrc-run

name={{.Name}}
command="{{.Executable}}"
command_args="{{range .Arguments}} {{.}}{{end}}" 
command_background="yes"

start_stop_daemon_args="--env {{.RunModeEnv}}=1"
pidfile="/var/run/$name.pid"

depend() {
	use net logger
	provide dns
}
`
