// Package bsd implements the BSD init system.

package bsd

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
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

	err := s.service("status")
	if internal.ExitCode(err) == 1 {
		return service.StatusStopped, nil
	} else if err != nil {
		return service.StatusUnknown, err
	}
	return service.StatusRunning, nil
}

func (s Service) Start() error {
	return s.service("start")
}

func (s Service) Stop() error {
	return s.service("stop")
}

func (s Service) Restart() error {
	return s.service("restart")
}

func (s Service) service(action string) error {
	name := s.Name
	if strings.HasSuffix(s.Path, ".sh") {
		// Pfsense needs a .sh suffix
		name += ".sh"
	}
	return internal.Run("service", name, action)
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

load_rc_config $name
run_rc_command "$1"
`
