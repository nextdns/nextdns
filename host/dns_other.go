//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows
// +build !darwin,!linux,!freebsd,!openbsd,!netbsd,!dragonfly,!windows

package host

import "errors"

func DNS() []string {
	return nil
}

func SetDNS(dns string, port uint16) error {
	_ = dns
	_ = port
	return errors.New("platform not supported")
}

func ResetDNS() error {
	return errors.New("platform not supported")
}
