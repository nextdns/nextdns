// +build !darwin,!linux

package dnsconf

import "errors"

func Get() ([]string, error) {
	return "", errors.New("platform not supported")
}
