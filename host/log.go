// +build !darwin,!linux

package host

import (
	"errors"
	"io"
)

func ReadLog(process string) (io.Reader, error) {
	return nil, errors.New("not implemented")
}
