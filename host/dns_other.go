// +build !darwin,!linux,!freebsd,!openbsd,!netbsd,!dragonfly

package host

import "errors"

func DNS() []string {
	return nil
}

func SetDNS(dns string) error {
	return errors.New("platform not supported")
}

func ResetDNS() error {
	return errors.New("platform not supported")
}
