package host

import (
	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/windows"
)

func NewService(c service.Config) (service.Service, error) {
	return windows.New(c)
}
