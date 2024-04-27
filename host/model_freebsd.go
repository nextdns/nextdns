package host

import "os/exec"

func Model() string {
	if b, err := exec.Command("sysctl", "-n", "kern.version").Output(); err == nil && len(b) > 0 {
		return string(b)
	}
	return "FreeBSD"
}
