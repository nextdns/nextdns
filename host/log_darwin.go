package host

import (
	"os/exec"
)

func ReadLog(process string) ([]byte, error) {
	return exec.Command("grep", process, "/var/log/system.log").Output()
}
