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
	// Merlin
	if _, err := os.Stat("/jffs/syslog.log"); err == nil {
		return exec.Command("grep", fmt.Sprintf(` %s\(:\|\[\)`, name), "/jffs/syslog.log").Output()
	}
	// OpenWRT
	if _, err := exec.LookPath("logread"); err == nil {
		b, err := exec.Command("logread", "-e", name).Output()
		if err == nil {
			return b, err
		}
	}
	// Systemd
	if _, err := exec.LookPath("journalctl"); err == nil {
		b, err := exec.Command("journalctl", "-q", "-b", "-u", name).Output()
		// If journalctl returns not output, try with another logging system.
		if err != nil || len(b) > 0 {
			return b, err
		}
	}
	// Pre-systemd
	if _, err := os.Stat("/var/log/messages"); err == nil {
		return exec.Command("grep", fmt.Sprintf(` %s\(:\|\[\)`, name), "/var/log/messages").Output()
	}
	return nil, errors.New("not supported")
}
