package vyos

import (
	"os"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/systemd"
)

func isVyOS() bool {
	if st, err := os.Stat("/config/scripts/"); err == nil && st.IsDir() {
		if _, err = os.Stat("/usr/libexec/vyos/init/vyos-router"); err == nil {
			return true
		}
	}
	return false
}

func New(c service.Config) (systemd.Service, error) {
	if !isVyOS() {
		return systemd.Service{}, service.ErrNotSupported
	}
	s, err := systemd.New(c)
	if err != nil {
		return s, err
	}
	s.ConfigFileStorer = service.ConfigFileStorer{File: "/config/nextdns/" + c.Name + ".conf"}
	s.Path = "/config/nextdns/" + c.Name + ".service"
	return s, nil
}
