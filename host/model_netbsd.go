package host

import (
	"bytes"
	"os/exec"
)

func Model() string {
	if b, err := exec.Command("sysctl", "-n", "kern.version").Output(); err == nil && len(b) > 0 {
		return string(bytes.TrimSpace(b))
	}
	return "NetBSD"
}
