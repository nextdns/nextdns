//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package host

import (
	"errors"
	"io"
)

func newServiceLogger(name string) (Logger, error) {
	return nil, errors.New("not implemented")
}

func ReadLog(process string) (io.Reader, error) {
	return nil, errors.New("not implemented")
}

func FollowLog(name string) error {
	return errors.New("-f/--follow not implemented")
}
