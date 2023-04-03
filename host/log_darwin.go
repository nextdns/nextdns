package host

import (
	"errors"
	"os/exec"
)

func newServiceLogger(name string) (Logger, error) {
	return newSyslogLogger(name)
}

func ReadLog(process string) ([]byte, error) {
	return exec.Command("grep", process, "/var/log/system.log").Output()
}

func FollowLog(name string) error {
	return errors.New("-f/--follow not implemented")
}
