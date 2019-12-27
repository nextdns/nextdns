package host

import (
	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/launchd"
)

func NewService(c service.Config) (service.Service, error) {
	return launchd.New(c)
}
