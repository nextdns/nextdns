// Package vyos implements the VyOS init system.

package vyos

import (
	"bufio"
	"bytes"
	"fmt"
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
	PostCfg_Script string
}

func New(c service.Config) (Service, error) {
	//if st, err := os.Stat("/config/scripts/"); err != nil || !st.IsDir() {
	//	if _, err = os.Stat("/usr/libexec/vyos/init/vyos-router"); err != nil {
	//		return Service{}, service.ErrNotSupported
	//	}
	//}
	ep, err := os.Executable()
	if err != nil {
		return Service{}, err
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: ep + ".conf"},
		Path:             "/config/scripts/" + c.Name + ".sh",
		PostCfg_Script:   "/config/scripts/vyos-postconfig-bootup.script",	
	}, nil
}

func (s Service) Install() error {
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config); err != nil {
		return err
	}
	
	if err := addLine(s.PostCfg_Script, s.Path); err != nil {
		return err
	}
	return nil
}

func (s Service) Uninstall() error {
	_ = removeLine(s.PostCfg_Script, s.Path)
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
		return os.WriteFile(file, []byte("#!/bin/sh\n"+line+"\n"), 0755)
	}
	if err != nil {
		return err
	}
	if found {
		return service.ErrAlreadyInstalled
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(bytes.NewReader(b))
	firstLine := true
	for s.Scan() {
		l := s.Text()
		if firstLine {
			firstLine = false
			if strings.HasPrefix(l, "#!") {
				_, err = fmt.Fprintf(f, "%s\n\n%s\n", l, line)
			} else {
				// missing shebang
				_, err = fmt.Fprintf(f, "#!/bin/sh\n\n%s\n%s\n", line, l)
			}
		} else {
			_, err = fmt.Fprintln(f, l)
		}
		if err != nil {
			return err
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	if firstLine {
		// Empty file
		return os.WriteFile(file, []byte("#!/bin/sh\n"+line+"\n"), 0755)
	}
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
	return os.WriteFile(file, out, 0755)
}

var tmpl = `#!/bin/sh

name="{{.Name}}"
exe="{{.Executable}}"
cmd="$exe{{range .Arguments}} {{.}}{{end}}"
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

		# Install a symlink of the service into the path if not already present
		if [ -z "$(command -v $(basename $exe))" ]; then
			ln -s "$exe" "/usr/bin/$(basename $exe)"
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
