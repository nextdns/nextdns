// Package windows implements the windows service management process.

// +build windows

package windows

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/nextdns/nextdns/host/service"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
}

func New(c service.Config) (Service, error) {
	ep, err := os.Executable()
	if err != nil {
		return Service{}, err
	}
	confPath := filepath.Join(filepath.Dir(ep), c.Name+".conf")
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: confPath},
	}, nil
}

func (s Service) Install() error {
	ep, err := exePath()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	srv, err := m.OpenService(s.Name)
	if err == nil {
		srv.Close()
		return service.ErrAlreadyInstalled
	}
	srv, err = m.CreateService(s.Name, ep, mgr.Config{
		DisplayName: s.DisplayName,
		Description: s.Description,
		StartType:   mgr.StartAutomatic,
	}, s.Arguments...)
	if err != nil {
		return err
	}
	defer srv.Close()
	err = srv.SetRecoveryActions([]mgr.RecoveryAction{
		mgr.RecoveryAction{
			Type:  mgr.ServiceRestart,
			Delay: 5 * time.Second,
		},
	}, syscall.INFINITE)
	if err != nil {
		return err
	}
	return nil

}

func (s Service) Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	srv, err := m.OpenService(s.Name)
	if err != nil {
		return service.ErrNoInstalled
	}
	defer srv.Close()
	err = srv.Delete()
	if err != nil {
		return err
	}
	return nil
}

func (s Service) Status() (service.Status, error) {
	m, err := mgr.Connect()
	if err != nil {
		return service.StatusUnknown, err
	}
	defer m.Disconnect()

	srv, err := m.OpenService(s.Name)
	if err != nil {
		if err.Error() == "The specified service does not exist as an installed service." {
			return service.StatusNotInstalled, nil
		}
		return service.StatusUnknown, err
	}

	status, err := srv.Query()
	if err != nil {
		return service.StatusUnknown, err
	}

	switch status.State {
	case svc.StartPending, svc.Running:
		return service.StatusRunning, nil
	case svc.PausePending, svc.Paused, svc.ContinuePending, svc.StopPending, svc.Stopped:
		return service.StatusStopped, nil
	default:
		return service.StatusUnknown, fmt.Errorf("unknown status %v", status)
	}
}

func (s Service) Start() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	srv, err := m.OpenService(s.Name)
	if err != nil {
		return err
	}
	defer srv.Close()
	return srv.Start()
}

func (s Service) Stop() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	srv, err := m.OpenService(s.Name)
	if err != nil {
		return fmt.Errorf("could not access service: %v", err)
	}
	defer srv.Close()
	status, err := srv.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("could not send control=%d: %v", svc.Stop, err)
	}
	timeout := time.Now().Add(10 * time.Second)
	for status.State != svc.Stopped {
		if timeout.Before(time.Now()) {
			return fmt.Errorf("timeout waiting for service to go to state=%d", svc.Stopped)
		}
		time.Sleep(300 * time.Millisecond)
		status, err = srv.Query()
		if err != nil {
			return fmt.Errorf("could not retrieve service status: %v", err)
		}
	}
	return nil
}

func (s Service) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}
	return s.Start()
}

func exePath() (string, error) {
	prog := os.Args[0]
	p, err := filepath.Abs(prog)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(p)
	if err == nil {
		if !fi.Mode().IsDir() {
			return p, nil
		}
		err = fmt.Errorf("%s is directory", p)
	}
	if filepath.Ext(p) == "" {
		p += ".exe"
		fi, err := os.Stat(p)
		if err == nil {
			if !fi.Mode().IsDir() {
				return p, nil
			}
			err = fmt.Errorf("%s is directory", p)
		}
	}
	return "", err
}
