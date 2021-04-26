// Package runit implements the runit init system.

package runit

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os"
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
	if b, _ := ioutil.ReadFile("/proc/1/comm"); !bytes.Equal(b, []byte("runit\n")) {
		return Service{}, service.ErrNotSuported
	}

	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/etc/" + c.Name + ".conf"},
		Path:             "/etc/sv/" + c.Name + "/run",
	}, nil
}

func (s Service) Install() error {
	if err := internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config); err != nil {
		return err
	}
	if err := os.Symlink(strings.TrimSuffix(s.Path, "/run"), "/etc/runit/runsvdir/current/" + s.Config.Name); err != nil {
		return err
	}
	return nil
}


func (s Service) Uninstall() error {
	if err := os.RemoveAll("/etc/runit/runsvdir/current/" + s.Config.Name); err != nil {
		return err
	}
	if err := os.RemoveAll(strings.TrimSuffix(s.Path, "/run")); err != nil {
		return err
	}
	return nil
}

func (s Service) Status() (service.Status, error) {
	out, err := internal.RunOutput("sv", "s", s.Name)
	if err != nil {
		if os.Geteuid() != 0 {
			return service.StatusUnknown, errors.New("permission denied")
		}
		return service.StatusUnknown, err
	}

	switch {
	case strings.HasPrefix(out, "run"):
		return service.StatusRunning, nil
	case strings.HasPrefix(out, "down"):
		return service.StatusStopped, nil
	default:
		return service.StatusNotInstalled, nil
	}
}

func (s Service) Start() error {
	err := internal.Run("sv", "up", s.Name)
	if err != nil {
		if os.Geteuid() != 0 {
			return errors.New("permission denied")
		}
	}
	return err
}

func (s Service) Stop() error {
	err := internal.Run("sv", "down", s.Name)
	if err != nil {
		if os.Geteuid() != 0 {
			return errors.New("permission denied")
		}
	}
	return err
}

func (s Service) Restart() error {
	err := internal.Run("sv", "restart", s.Name)
	if err != nil {
		if os.Geteuid() != 0 {
			return errors.New("permission denied")
		}
	}
	return err
}

var tmpl = `#!/bin/sh
exec {{.Executable}}{{range .Arguments}} {{.}}{{end}}
`
