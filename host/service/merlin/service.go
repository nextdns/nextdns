// Package merlin implements the ASUS-Merlin init system.

package merlin

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path       string
	JFFSScript string
}

func New(c service.Config) (Service, error) {
	if b, err := exec.Command("uname", "-o").Output(); err != nil ||
		!strings.HasPrefix(string(b), "ASUSWRT-Merlin") {
		return Service{}, service.ErrNotSuported
	}
	ep, err := os.Executable()
	if err != nil {
		return Service{}, err
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: ep + ".conf"},
		Path:             ep + ".init",
		JFFSScript:       "/jffs/scripts/services-start",
	}, nil
}

func (s Service) Install() error {
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config); err != nil {
		return err
	}

	out, err := internal.RunOutput("nvram", "get", "jffs2_scripts")
	if err != nil {
		return fmt.Errorf("check jffs2_scripts: %v", err)
	}
	if !strings.HasPrefix(out, "1") {
		if err := internal.Run("nvram", "set", "jffs2_scripts=1"); err != nil {
			return fmt.Errorf("enable jffs2_scripts: %v", err)
		}
		if err := internal.Run("nvram", "commit"); err != nil {
			return fmt.Errorf("nvram commit: %v", err)
		}
	}
	if err := addLine(s.JFFSScript, s.Path+" start"); err != nil {
		return err
	}
	return nil
}

func (s Service) Uninstall() error {
	_ = removeLine(s.JFFSScript, s.Path+" start")
	if err := os.Remove(s.Path); err != nil {
		if os.IsNotExist(err) {
			return service.ErrNoInstalled
		}
		return err
	}
	return nil
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

func excludeLine(file, line string) (found bool, out []byte, err error) {
	f, err := os.Open(file)
	if err != nil {
		return false, nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if s.Text() == line {
			found = true
		} else {
			out = append(out, s.Bytes()...)
			out = append(out, '\n')
		}
	}
	if err := s.Err(); err != nil {
		return false, nil, err
	}
	return
}

func addLine(file, line string) error {
	found, _, err := excludeLine(file, line)
	if os.IsNotExist(err) {
		return ioutil.WriteFile(file, []byte("#!/bin/sh\n"+line+"\n"), 0755)
	}
	if err != nil {
		return err
	}
	if found {
		return service.ErrAlreadyInstalled
	}
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, "\n"+line)
	return err
}

func removeLine(file, line string) error {
	found, out, err := excludeLine(file, line)
	if err != nil {
		return err
	}
	if !found {
		return service.ErrNoInstalled
	}
	if bytes.Equal(bytes.TrimSpace(out), []byte("#!/bin/sh")) {
		return os.Remove(file)
	}
	return ioutil.WriteFile(file, out, 0755)
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

log() {
	logger -s -t "${name}.init" "$@"
}

setup_tz() {
	tz="$(nvram get time_zone)"
	tz_dir="/jffs/zoneinfo"
	tz_file="$tz_dir/$tz"
	tz_url="https://github.com/nextdns/nextdns/raw/master/router/merlin/tz/$tz"
	if [ "$(readlink /etc/localtime)" != "$tz_file" ]; then
		if [ -f "$tz_file" ]; then
			ln -sf "$tz_file" /etc/localtime
		else
			mkdir -p "$tz_dir"
			if curl -sLo "$tz_file" "$tz_url"; then
				ln -sf "$tz_file" /etc/localtime
			fi
		fi
	fi
}

case "$1" in
	start)
		if is_running; then
			log "Already started"
		else
			if [ -f /rom/ca-bundle.crt ]; then
				# John’s fork 39E3j9527 has trust store in non-standard location
				export SSL_CERT_FILE=/rom/ca-bundle.crt
			fi
			setup_tz
			unset TZ
			export {{.RunModeEnv}}=1
			$cmd &
			echo $! > "$pid_file"
			if ! is_running; then
				log "Unable to start"
				exit 1
			fi
		fi
	;;
	stop)
		if is_running; then
			kill $(get_pid)
			for i in 1 2 3 4 5 6 7 8 9 10; do
				if ! is_running; then
					break
				fi
				sleep 1
			done
			if is_running; then
				log "Not stopped; may still be shutting down or shutdown may have failed"
				exit 1
			else
				log "Stopped"
				if [ -f "$pid_file" ]; then
					rm "$pid_file"
				fi
			fi
		else
			log "Not running"
		fi
	;;
	restart)
		$0 stop
		if is_running; then
			log "Unable to stop, will not attempt to start"
			exit 1
		fi
		$0 start
	;;
	status)
		if is_running; then
			log "Running"
		else
			log "Stopped"
			exit 1
		fi
	;;
	*)
	log "Usage: $0 {start|stop|restart|status}"
	exit 1
	;;
esac
exit 0
`
