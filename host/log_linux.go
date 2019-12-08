package host

import "os/exec"

func ReadLog(process string) ([]byte, error) {
	return exec.Command("journalctl", "-b", "-u", process).Output()
}
