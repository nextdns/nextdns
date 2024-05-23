package host

import (
	"bytes"
	"os/exec"
)

func Model() string {
	if b, err := exec.Command("uname", "-sr").Output(); err == nil && len(b) > 0 {
		return string(bytes.TrimSpace(b))
	}
	return "FreeBSD"
}
