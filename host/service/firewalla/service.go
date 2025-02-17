// Package firewalla implements the Firewalla package init system.

package firewalla

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path string
}

const confDir = "/home/pi/.firewalla/config"
const initDir = "/home/pi/.firewalla/config/post_main.d"

func New(c service.Config) (Service, error) {
	if _, err := os.Stat("/etc/firewalla_release"); err != nil {
		return Service{}, service.ErrNotSupported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: filepath.Join(confDir, c.Name+".conf")},
		Path:             filepath.Join(initDir, c.Name+".sh"),
	}, nil
}

func (s Service) Install() error {
	_ = os.MkdirAll(initDir, 0755)
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config); err != nil {
		return err
	}
	return nil
}

func (s Service) Uninstall() error {
	if err := os.Remove(s.Path); err != nil {
		if os.IsNotExist(err) {
			return service.ErrNoInstalled
		}
		return err
	}
	return nil
}

func (s Service) Status() (service.Status, error) {
	out, err := internal.RunOutput(s.Path, "status")
	switch {
	case strings.HasPrefix(out, "Running"):
		return service.StatusRunning, nil
	case strings.HasPrefix(out, "Stopped"):
		return service.StatusStopped, nil
	default:
		if err != nil {
			return service.StatusUnknown, err
		}
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

var tmpl = `#!/bin/bash

cmd="{{.Executable}}{{range .Arguments}} {{.}}{{end}}"

name={{.Name}}
pid_file="/home/pi/firewalla/run/$name.pid"

get_pid() {
	cat "$pid_file"
}

is_running() {
	[ -f "$pid_file" ] && ps $(get_pid) > /dev/null 2>&1
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
			sudo sh -c "
					export {{.RunModeEnv}}=1
					$cmd </dev/null >/dev/null 2>&1 &
					echo \$! > "$pid_file"
			"
			if ! is_running; then
				echo "Unable to start"
				exit 1
			fi
		fi
	;;
	stop)
		if is_running; then
			echo -n "Stopping $name.."
			sudo kill $(get_pid)
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
					sudo rm "$pid_file"
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
