// Package ubios implements the Unifi OS init system.
//
// On ubios the base system does not provide a hook to start custom services.
// Although, the base system starts a podman (Docker like) container with a
// standard Debian stretch with systemd.
//
// The trick is thus to install a systemd init that will start the service from
// inside the container.

package ubios

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path       string
	Executable string
}

const containerName = "unifi-os"

func New(c service.Config) (Service, error) {
	if b, _ := ioutil.ReadFile("/etc/os-release"); !bytes.Contains(b, []byte("\nID=ubios\n")) &&
		// The service is started from within the container which is not id as
		// ubios, so init script sets the UBIOS=1 env so the detection can be
		// forced.
		os.Getenv("UBIOS") != "1" {
		return Service{}, service.ErrNotSuported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/data/" + c.Name + ".conf"},
		Path:             "/etc/systemd/system/" + c.Name + ".service",
		Executable:       "/data/nextdns",
	}, nil
}

func (s Service) Install() error {
	if out, _ := exec.Command("podman", "cp", containerName+":"+s.Path, "-").Output(); len(out) > 0 {
		return service.ErrAlreadyInstalled
	}
	if err := s.createPodmanSystemdServiceFile(); err != nil {
		return err
	}
	if err := systemctl("enable", s.Name+".service"); err != nil {
		return err
	}
	return systemctl("daemon-reload")
}

func (s Service) createPodmanSystemdServiceFile() error {
	// First copy the systemd script from the container if exists, then write it
	// as if it were local then upload the result to the container.
	tmpFile, err := tmpPath()
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(tmpFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err = t.Execute(f, struct {
		service.Config
		Executable string
		RunModeEnv string
	}{
		s.Config,
		s.Executable,
		service.RunModeEnv,
	}); err != nil {
		return err
	}

	if err := internal.Run("podman", "cp", tmpFile, containerName+":"+s.Path); err != nil {
		return err
	}

	return nil
}

func (s Service) Uninstall() error {
	err := systemctl("disable", s.Name+".service")
	if err != nil {
		return err
	}
	if err := internal.Run("podman", "exec", containerName, "rm", "-f", s.Path); err != nil {
		return err
	}
	return nil
}

func (s Service) Status() (service.Status, error) {
	outB, err := exec.Command("podman", "exec", containerName, "systemctl", "is-active", s.Name).Output()
	if internal.ExitCode(err) == 0 && err != nil {
		return service.StatusUnknown, err
	}
	out := string(outB)
	switch {
	case strings.HasPrefix(out, "active"):
		return service.StatusRunning, nil
	case strings.HasPrefix(out, "inactive"):
		return service.StatusStopped, nil
	case strings.HasPrefix(out, "failed"):
		return service.StatusUnknown, errors.New("service in failed state")
	default:
		return service.StatusNotInstalled, nil
	}
}

func (s Service) Start() error {
	return systemctl("start", s.Name+".service")
}

func (s Service) Stop() error {
	return systemctl("stop", s.Name+".service")
}

func (s Service) Restart() error {
	return systemctl("restart", s.Name+".service")
}

func tmpPath() (string, error) {
	tf, err := ioutil.TempFile("/tmp", "nextdns")
	if err != nil {
		return "", err
	}
	defer os.Remove(tf.Name())
	return tf.Name(), nil
}

func systemctl(args ...string) error {
	args = append([]string{"exec", containerName, "systemctl"}, args...)
	return internal.Run("podman", args...)
}

var tmpl = `[Unit]
Description={{.Description}}
ConditionFileIsExecutable={{.Executable}}
After=network.target
Before=nss-lookup.target
Wants=nss-lookup.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
Environment={{.RunModeEnv}}=1
Environment=UBIOS=1
ExecStart={{.Executable}}{{range .Arguments}} {{.}}{{end}}
RestartSec=120
LimitMEMLOCK=infinity

[Install]
WantedBy=multi-user.target
`
