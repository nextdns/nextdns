// +build !darwin,!linux,!freebsd,!openbsd,!netbsd,!dragonfly

package host

import "errors"

func DNS() ([]string, error) {
	return nil, errors.New("platform not supported")
}

func SetDNS(dns string) error {
	return errors.New("platform not supported")
}

func ResetDNS() error {
	return errors.New("platform not supported")
}
