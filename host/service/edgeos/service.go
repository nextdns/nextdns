// Package edgeos implements the Ubiquiti EdgeOS & VyOS init system.

package edgeos

import (
	"os"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path string
}

func New(c service.Config) (Service, error) {
	if st, err := os.Stat("/config/scripts/post-config.d"); err != nil || !st.IsDir() {
		return Service{}, service.ErrNotSuported
	}
	ep, err := os.Executable()
	if err != nil {
		return Service{}, err
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: ep + ".conf"},
		Path:             "/config/scripts/post-config.d/" + c.Name + ".sh",
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
			for i in $(seq 1 10)
			do
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
