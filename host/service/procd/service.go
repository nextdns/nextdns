// Package procd implements the OpenWRT PROCD init system.

package procd

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	Path string
}

func New(c service.Config) (Service, error) {
	if dest, err := os.Readlink("/proc/1/exe"); err != nil || dest != "/sbin/procd" {
		return Service{}, service.ErrNotSuported
	}
	return Service{
		Config: c,
		Path:   "/etc/init.d/" + c.Name,
	}, nil
}

func (s Service) Install() error {
	if _, err := uci("get", s.uciEntryName("enabled")); errors.Is(err, errUCIEntryNotFound) {
		// First install, setup some required defaults
		yes := true
		if err := s.SaveConfig(map[string]service.ConfigEntry{
			"enabled":            service.ConfigFlag{Value: &yes},
			"setup_router":       service.ConfigFlag{Value: &yes},
			"report_client_info": service.ConfigFlag{Value: &yes},
		}); err != nil {
			return err
		}
	}

	if err := internal.CreateWithTemplate(s.Path, tmpl, 0755, s.Config); err != nil {
		return err
	}

	var err error
	for _, i := range [...]string{"2", "3", "4", "5"} {
		if err = os.Symlink(s.Path, "/etc/rc"+i+".d/S50"+s.Name); err != nil {
			continue
		}
	}
	for _, i := range [...]string{"0", "1", "6"} {
		if err = os.Symlink(s.Path, "/etc/rc"+i+".d/K02"+s.Name); err != nil {
			continue
		}
	}

	return nil
}

func (s Service) Uninstall() error {
	if err := os.Remove(s.Path); err != nil {
		return err
	}
	return nil
}

func (s Service) Status() (service.Status, error) {
	if _, err := os.Stat(s.Path); os.IsNotExist(err) {
		return service.StatusNotInstalled, nil
	}
	b, err := ioutil.ReadFile("/var/run/" + s.Name + ".pid")
	if err != nil {
		if os.IsNotExist(err) {
			return service.StatusStopped, nil
		}
		return service.StatusUnknown, err
	}
	pid, err := strconv.ParseInt(string(bytes.TrimSpace(b)), 10, 32)
	if err != nil {
		return service.StatusUnknown, err
	}
	if _, err := os.FindProcess(int(pid)); err == nil {
		return service.StatusRunning, nil
	}
	return service.StatusStopped, nil
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

func (s Service) SaveConfig(c map[string]service.ConfigEntry) error {
	cp := s.confPath()
	if _, err := os.Stat(cp); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := ioutil.WriteFile(cp, []byte{}, 0644); err != nil {
			return err
		}
		if _, err := uci("set", s.Name+".main="+s.Name); err != nil {
			return err
		}
	}

	for name, entry := range c {
		name = s.uciEntryName(name)
		if entry, ok := entry.(service.ConfigListEntry); ok {
			if _, err := uci("delete", name); err != nil && !errors.Is(err, errUCIEntryNotFound) {
				return err
			}
			for _, value := range entry.Strings() {
				if _, err := uci("add_list", name+"="+uciValue(value)); err != nil {
					return err
				}
			}
			continue
		}
		if value := entry.String(); value != "" {
			if _, err := uci("set", name+"="+uciValue(value)); err != nil {
				return err
			}
		} else {
			if _, err := uci("delete", name); err != nil && !errors.Is(err, errUCIEntryNotFound) {
				return err
			}
		}
	}

	_, err := uci("commit")
	return err
}

func (s Service) LoadConfig(c map[string]service.ConfigEntry) error {
	for name, entry := range c {
		name = s.uciEntryName(name)
		out, err := uci("-d|-|", "get", name)
		if err != nil {
			if errors.Is(err, errUCIEntryNotFound) {
				continue
			}
			return err
		}
		if _, ok := entry.(service.ConfigListEntry); ok {
			for _, value := range strings.Split(out, "|-|") {
				if strings.HasPrefix(value, "'") {
					value = strings.Trim(value, "'")
				}
				if err := entry.Set(value); err != nil {
					return err
				}
			}
		} else {
			if err := entry.Set(out); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s Service) uciEntryName(name string) string {
	return s.Name + ".main." + strings.ReplaceAll(name, "-", "_")
}

func (s Service) confPath() string {
	return "/etc/config/" + s.Name
}

var tmpl = `#!/bin/sh /etc/rc.common

USE_PROCD=1

# starts after network starts
START=21
# stops before networking stops
STOP=89

PROG={{.Executable}}

start_service() {
	config_load {{.Name}}
	config_get_bool enabled main enabled "1"
	if [ "$enabled" = "1" ]; then
		procd_open_instance
		procd_set_param env {{.RunModeEnv}}=1
		procd_set_param command $PROG{{range .Arguments}} {{.}}{{end}}
		procd_set_param pidfile /var/run/{{.Name}}.pid
		procd_set_param stdout 1
		procd_set_param stderr 1
		procd_set_param respawn
		procd_close_instance
	fi
}

service_triggers() {
	procd_add_reload_trigger "{{.Name}}"
}`
