// Package entware implements the Entware package init system.

package entware

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
	if _, err := os.Stat("/opt/etc/init.d/rc.func"); err != nil {
		return Service{}, service.ErrNotSuported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/opt/etc/" + c.Name + ".conf"},
		Path:             "/opt/etc/init.d/S09" + c.Name,
	}, nil
}

func (s Service) Install() error {
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
	if _, err := os.Stat(s.Path); os.IsNotExist(err) {
		return service.StatusNotInstalled, nil
	}

	err := internal.Run(s.Path, "check")
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

ENABLED=yes
PROCS={{.Name}}
ARGS=""
PREARGS=""
DESC=$PROCS
PATH=/opt/sbin:/opt/bin:/opt/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

. /opt/etc/init.d/rc.func
`
