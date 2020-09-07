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
	// OpenWRT
	if _, err := exec.LookPath("logread"); err == nil {
		return exec.Command("logread", "-e", name).Output()
	}
	// Ubios
	if os.Getenv("UBIOS") != "1" {
		return exec.Command("podman", "exec", "unifi-os", "journalctl", "-b", "-u", name).Output()
	}
	// Systemd
	if _, err := exec.LookPath("journalctl"); err == nil {
		return exec.Command("journalctl", "-b", "-u", name).Output()
	}
	// Merlin
	if _, err := os.Stat("/jffs/syslog.log"); err == nil {
		return exec.Command("grep", fmt.Sprintf(` %s\(:\|\[\)`, name), "/jffs/syslog.log").Output()
	}
	// Pre-systemd
	if _, err := os.Stat("/var/log/messages"); err == nil {
		return exec.Command("grep", fmt.Sprintf(` %s\(:\|\[\)`, name), "/var/log/messages").Output()
	}
	return nil, errors.New("not supported")
}
