// Package ddwrt implements the dd-wrt init system.

package ddwrt

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
	if b, err := exec.Command("uname", "-o").Output(); err != nil ||
		!strings.HasPrefix(string(b), "DD-WRT") {
		return Service{}, service.ErrNotSupported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/jffs/etc/" + c.Name + ".conf"},
		Path:             "/jffs/etc/config/" + c.Name + ".startup",
	}, nil
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

	err := internal.Run(s.Path, "status")
	if internal.ExitCode(err) == 1 {
		return service.StatusStopped, nil
	} else if err != nil {
		return service.StatusUnknown, err
	}
	return service.StatusRunning, nil
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

name="{{.Name}}"
cmd="{{.Executable}}{{range .Arguments}} {{.}}{{end}}"
pid_file="/tmp/$name.pid"

get_pid() {
	cat "$pid_file"
}

is_running() {
	test -f "$pid_file" && ps | grep -q "^ *$(get_pid) "
}

action=$1
if [ -z "$action" ]; then
	action=start
fi

case "$action" in
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
