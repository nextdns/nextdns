//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows
// +build !darwin,!linux,!freebsd,!openbsd,!netbsd,!dragonfly,!windows

package host

import "github.com/nextdns/nextdns/host/service"

func NewService(c service.Config) (service.Service, error) {
	_ = c
	return nil, service.ErrNotSupported
}
