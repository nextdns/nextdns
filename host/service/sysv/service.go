// Package sysv implements the System V init system.

package sysv

import (
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
	if _, err := exec.LookPath("service"); err != nil {
		return Service{}, service.ErrNotSuported
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

	var err error
	for _, i := range [...]string{"2", "3", "4", "5"} {
		if err = os.Symlink(s.Path, "/etc/rc"+i+".d/S50"+s.Name); err != nil {
			continue
		}
	}
	for _, i := range [...]string{"0", "1", "6"} {
		if err = os.Symlink(s.Path, "/etc/rc"+i+".d/K02"+s.Name); err != nil {
			continue
		}
	}

	return nil
}

func (s Service) Uninstall() error {
	if err := os.Remove(s.Path); err != nil {
		return err
	}
	return nil
}

func (s Service) Status() (service.Status, error) {
	_, out, err := internal.RunOutput("service", s.Name, "status")
	if err != nil {
		return service.StatusUnknown, err
	}

	switch {
	case strings.HasPrefix(out, "Running"):
		return service.StatusRunning, nil
	case strings.HasPrefix(out, "Stopped"):
		return service.StatusStopped, nil
	default:
		return service.StatusNotInstalled, nil
	}
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
# For RedHat and cousins:
# chkconfig: - 99 01
# description: {{.Description}}
# processname: {{.Executable}}

### BEGIN INIT INFO
# Provides:          {{.Executable}}
# Required-Start:
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: {{.DisplayName}}
# Description:       {{.Description}}
### END INIT INFO

cmd="{{.Executable}}{{range .Arguments}} {{.}}{{end}}"

name={{.Name}}
pid_file="/var/run/$name.pid"

[ -e /etc/sysconfig/$name ] && . /etc/sysconfig/$name

get_pid() {
	cat "$pid_file"
}

is_running() {
	[ -f "$pid_file" ] && ps $(get_pid) > /dev/null 2>&1
}

case "$1" in
	start)
		if is_running; then
			echo "Already started"
		else
			echo "Starting $name"
			export {{.RunModeEnv}}=1
			($cmd 2>&1 & echo $! >&3) 3> "$pid_file" | logger -s -t "${name}.init" &
			if ! is_running; then
				echo "Unable to start"
				exit 1
			fi
		fi
	;;
	stop)
		if is_running; then
			echo -n "Stopping $name.."
			kill $(get_pid)
			for i in 1 2 3 4 5 6 7 8 9 10; do
				if ! is_running; then
					break
				fi
				echo -n "."
				sleep 1
			done
			echo
			if is_running; then
				echo "Not stopped; may still be shutting down or shutdown may have failed"
				exit 1
			else
				echo "Stopped"
				if [ -f "$pid_file" ]; then
					rm "$pid_file"
				fi
			fi
		else
			echo "Not running"
		fi
	;;
	restart)
		$0 stop
		if is_running; then
			echo "Unable to stop, will not attempt to start"
			exit 1
		fi
		$0 start
	;;
	status)
		if is_running; then
			echo "Running"
		else
			echo "Stopped"
			exit 1
		fi
	;;
	*)
	echo "Usage: $0 {start|stop|restart|status}"
	exit 1
	;;
esac
exit 0
`
