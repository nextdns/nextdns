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
	b, err := exec.Command("grep", fmt.Sprintf(` %s\(:\|\[\)`, name), logFile).Output()
	if err != nil {
		var e *exec.ExitError
		// grep exits with code 1 when no lines match, which should not fail `nextdns log`.
		if errors.As(err, &e) && e.ExitCode() == 1 {
			return b, nil
		}
	}
	return b, err
}

func FollowLog(name string) error {
	return errors.New("-f/--follow not implemented")
}
