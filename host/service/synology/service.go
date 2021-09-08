// Package synology implements the Synology init system.

package synology

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
	if b, err := exec.Command("uname", "-u").Output(); err != nil ||
		!strings.HasPrefix(string(b), "synology") {
		return Service{}, service.ErrNotSupported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/usr/local/etc/" + c.Name + ".conf"},
		Path:             "/usr/local/etc/rc.d/" + c.Name + ".sh",
	}, nil
}

func (Service) Type() string {
	return "synology"
}

func (s Service) Install() error {
	return internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config)
}

func (s Service) Uninstall() error {
	return os.Remove(s.Path)
}

func (s Service) Status() (service.Status, error) {
	out, err := internal.RunOutput(s.Path, "status")
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
	return internal.Run(s.Path, "start")
}

func (s Service) Stop() error {
	return internal.Run(s.Path, "stop")
}

func (s Service) Restart() error {
	return internal.Run(s.Path, "restart")
}

var tmpl = `#!/bin/sh

cmd="{{.Executable}}{{range .Arguments}} {{.}}{{end}}"
name={{.Name}}
pid_file="/var/run/$name.pid"

[ -e /etc/sysconfig/$name ] && . /etc/sysconfig/$name

get_pid() {
	cat "$pid_file"
}

is_running() {
	if readlink /bin/ls 2>&1 | grep busybox > /dev/null ; then
		test -f "$pid_file" && ps | grep -q "^ *$(get_pid) "
	else
		[ -f "$pid_file" ] && ps $(get_pid) > /dev/null 2>&1
	fi
}

case "$1" in
	start)
		if is_running; then
			echo "Already started"
		else
			echo "Starting $name"
			export {{.RunModeEnv}}=1
			$cmd &
			echo $! > "$pid_file"
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
