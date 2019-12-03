// +build !darwin,!linux

package dnsconf

import "errors"

func Get() ([]string, error) {
	return nil, errors.New("platform not supported")
}
