package host

import (
	"os/exec"
	"strings"
)

func Model() string {
	cmd := exec.Command("wmic", "computersystem", "get", "model")
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	// Remove Model\r\n prefix.
	for len(b) > 0 {
		if b[0] == '\n' {
			return strings.TrimSpace(string(b[1:]))
		}
		b = b[1:]
	}
	return ""
}
