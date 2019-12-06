// +build darwin

package host

import (
	"os"
	"os/exec"
	"strings"
)

func Name() (string, error) {
	b, err := exec.Command("networksetup", "-getcomputername").Output()
	if err == nil {
		return string(b), nil
	}
	h, err := os.Hostname()
	return strings.TrimSuffix(h, ".local"), err
}
