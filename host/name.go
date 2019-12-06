// +build !darwin

package host

import (
	"os"
)

func Name() (string, error) {
	return os.Hostname()
}
