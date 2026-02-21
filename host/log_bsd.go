//go:build freebsd || openbsd || netbsd || dragonfly

package host

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func newServiceLogger(name string) (Logger, error) {
	return newSyslogLogger(name)
}

func ReadLog(name string) ([]byte, error) {
	logFile := "/var/log/messages"
	// pfSense
	if _, err := os.Stat("/var/log/system.log"); err == nil {
		logFile = "/var/log/system.log"
	}
	return exec.Command("grep", fmt.Sprintf(` %s\(:\|\[\)`, name), logFile).Output()
}

func FollowLog(name string) error {
	return errors.New("-f/--follow not implemented")
}
