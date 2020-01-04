// Package bsd implements the BSD init system.

package bsd

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path string
}

func New(c service.Config) (Service, error) {
	switch runtime.GOOS {
	case "freebsd", "openbsd", "netbsd", "dragonfly":
		if _, err := exec.LookPath("service"); err != nil {
			return Service{}, service.ErrNotSuported
		}
	default:
		return Service{}, service.ErrNotSuported
	}

	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/usr/local/etc/" + c.Name + ".conf"},
		Path:             path(c.Name),
	}, nil
}

func path(name string) string {
	if runtime.GOOS == "freebsd" {
		if b, err := ioutil.ReadFile("/etc/platform"); err == nil && bytes.HasPrefix(b, []byte("pfSense")) {
			// https://docs.netgate.com/pfsense/en/latest/development/executing-commands-at-boot-time.html
			return "/usr/local/etc/rc.d/" + name + ".sh"
		}
		return "/usr/local/etc/rc.d/" + name
	}
	return "/etc/rc.d/" + name
}

func (s Service) Install() error {
	return internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config)
}

func (s Service) Uninstall() error {
	return os.Remove(s.Path)
}

func (s Service) Status() (service.Status, error) {
	if _, err := os.Stat(s.Path); os.IsNotExist(err) {
		return service.StatusNotInstalled, nil
	}

	status, _, err := internal.RunCommand("service", false, s.Name, "status")
	if status == 1 {
		return service.StatusStopped, nil
	} else if err != nil {
		return service.StatusUnknown, err
	}
	return service.StatusRunning, nil
}

func (s Service) Start() error {
	return internal.Run("service", s.Name, "start")
}

func (s Service) Stop() error {
	return internal.Run("service", s.Name, "stop")
}

func (s Service) Restart() error {
	return internal.Run("service", s.Name, "restart")
}

var tmpl = `#!/bin/sh

# PROVIDE: {{.Name}}
# REQUIRE: SERVERS
# KEYWORD: shutdown

. /etc/rc.subr

name="{{.Name}}"
{{.Name}}_env="{{.RunModeEnv}}=1"
pidfile="/var/run/${name}.pid"
command="/usr/sbin/daemon"
daemon_args="-P ${pidfile} -r -t \"${name}: daemon\""
command_args="${daemon_args} {{.Executable}}{{range .Arguments}} {{.}}{{end}}"

run_rc_command "$1"
`
