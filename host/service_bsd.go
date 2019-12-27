// +build freebsd,openbsd,netbsd,dragonfly

package host

import (
	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/bsd"
)

func NewService(c service.Config) (service.Service, error) {
	return bsd.New(c)
}
