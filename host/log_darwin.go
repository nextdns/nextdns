package host

import (
	"os/exec"
)

func newServiceLogger(name string) (Logger, error) {
	return newSyslogLogger(name)
}

func ReadLog(process string) ([]byte, error) {
	return exec.Command("grep", process, "/var/log/system.log").Output()
}
