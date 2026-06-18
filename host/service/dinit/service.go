// Package dinit implements the dinit init system.

package dinit

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path    string
	EnvPath string
}

func New(c service.Config) (Service, error) {
	if b, _ := os.ReadFile("/proc/1/comm"); !bytes.Equal(b, []byte("dinit\n")) {
		return Service{}, service.ErrNotSupported
	}
	if _, err := exec.LookPath("dinitctl"); err != nil {
		return Service{}, service.ErrNotSupported
	}

	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/etc/" + c.Name + ".conf"},
		Path:             "/etc/dinit.d/" + c.Name,
		EnvPath:          "/etc/dinit.d/" + c.Name + ".env",
	}, nil
}

func (s Service) Install() error {
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0644, s.Config); err != nil {
		return err
	}
	if err := os.WriteFile(s.EnvPath, []byte(service.RunModeEnv+"=1\n"), 0644); err != nil {
		_ = os.Remove(s.Path)
		return err
	}
	if err := internal.Run("dinitctl", "enable", s.Name); err != nil {
		_ = os.Remove(s.Path)
		_ = os.Remove(s.EnvPath)
		return err
	}
	return nil
}

func (s Service) Uninstall() error {
	errDisable := internal.Run("dinitctl", "disable", s.Name)
	_ = os.Remove(s.EnvPath)
	if err := os.Remove(s.Path); err != nil {
		return err
	}
	return errDisable
}

func (s Service) Status() (service.Status, error) {
	out, err := internal.RunOutput("dinitctl", "status", s.Name)
	if err != nil {
		if os.Geteuid() != 0 {
			return service.StatusUnknown, errors.New("permission denied")
		}
		return service.StatusUnknown, err
	}

	switch {
	case strings.Contains(out, "STARTED"):
		return service.StatusRunning, nil
	case strings.Contains(out, "STOPPED"):
		return service.StatusStopped, nil
	default:
		return service.StatusNotInstalled, nil
	}
}

func (s Service) Start() error   { return ctl("start", s.Name) }
func (s Service) Stop() error    { return ctl("stop", s.Name) }
func (s Service) Restart() error { return ctl("restart", s.Name) }

func ctl(action, name string) error {
	if err := internal.Run("dinitctl", action, name); err != nil {
		if os.Geteuid() != 0 {
			return errors.New("permission denied")
		}
		return err
	}
	return nil
}

var tmpl = `type = process
command = {{.Executable}}{{range .Arguments}} {{.}}{{end}}
env-file = /etc/dinit.d/{{.Name}}.env
restart = true
`
