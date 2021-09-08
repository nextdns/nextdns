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
			return Service{}, service.ErrNotSupported
		}
	default:
		return Service{}, service.ErrNotSupported
	}

	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/usr/local/etc/" + c.Name + ".conf"},
		Path:             rcDaemonPath(c.Name),
	}, nil
}

func rcDaemonPath(name string) string {
	if runtime.GOOS == "freebsd" {
		if b, err := ioutil.ReadFile("/etc/platform"); err == nil && bytes.HasPrefix(b, []byte("pfSense")) {
			// https://docs.netgate.com/pfsense/en/latest/development/executing-commands-at-boot-time.html
			return "/usr/local/etc/rc.d/" + name + ".sh"
		}
		return "/usr/local/etc/rc.d/" + name
	}
	return "/etc/rc.d/" + name
}

func rcConfigPath(name string) string {
	return "/etc/rc.conf.d/" + name
}

func (s Service) Install() error {
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config); err != nil {
		return err
	}
	return internal.CreateWithTemplate(rcConfigPath(s.Name), rcConfTmpl, 0644, s.Config)
}

func (s Service) Uninstall() error {
	if err := os.Remove(s.Path); err != nil {
		return err
	}
	return os.Remove(rcConfigPath(s.Name))
}

func (s Service) Status() (service.Status, error) {
	if _, err := os.Stat(s.Path); os.IsNotExist(err) {
		return service.StatusNotInstalled, nil
	}

	// Under FreeBSD, if the service is disabled (nextdns_enable="NO")
	// `service status` will nevertheless return a `0` exit code,
	// suggesting the service is running.
	// Therefore, we should always call `service nextdns onestatus`
	// which will ensure the proper exit code in case the service
	// is stopped
	err := s.service("onestatus")
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

var rcConfTmpl = `# {{.Name}}
# Set this to "NO" to disable 
# the service but keep it installed
{{.Name}}_enable="YES"
`

var tmpl = `#!/bin/sh

# PROVIDE: {{.Name}}
# REQUIRE: SERVERS
# KEYWORD: shutdown

. /etc/rc.subr

name="{{.Name}}"
rcvar="{{.Name}}_enable"

{{.Name}}_env="{{.RunModeEnv}}=1"
pidfile="/var/run/${name}.pid"
command="/usr/sbin/daemon"
daemon_args="-P ${pidfile} -r -t \"${name}: daemon\""
command_args="${daemon_args} {{.Executable}}{{range .Arguments}} {{.}}{{end}}"

load_rc_config $name
run_rc_command "$1"
`
