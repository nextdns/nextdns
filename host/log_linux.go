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
		b, err := exec.Command("grep", fmt.Sprintf(` %s\(:\|\[\)`, name), "/jffs/syslog.log").Output()
		if err != nil {
			var e *exec.ExitError
			// grep exits with code 1 when no lines match, which should not fail `nextdns log`.
			if errors.As(err, &e) && e.ExitCode() == 1 {
				return b, nil
			}
		}
		return b, err
	}
	// OpenWRT
	if _, err := exec.LookPath("logread"); err == nil {
		b, err := exec.Command("logread", "-e", name).Output()
		if err == nil {
			return b, err
		}
	}
	// Firewalla
	if _, err := os.Stat("/etc/firewalla_release"); err == nil {
		b, err := exec.Command("sudo", "journalctl", "-q", "-b", "-t", name).Output()
		return b, err
	}
	// Systemd
	if _, err := exec.LookPath("journalctl"); err == nil {
		b, err := exec.Command("journalctl", "-q", "-b", "-u", name).Output()
		// If journalctl returns no output, try with another logging system.
		if err != nil || len(b) > 0 {
			return b, err
		}
	}
	// Pre-systemd
	if _, err := os.Stat("/var/log/messages"); err == nil {
		b, err := exec.Command("grep", fmt.Sprintf(` %s\(:\|\[\)`, name), "/var/log/messages").Output()
		if err != nil {
			var e *exec.ExitError
			// grep exits with code 1 when no lines match, which should not fail `nextdns log`.
			if errors.As(err, &e) && e.ExitCode() == 1 {
				return b, nil
			}
		}
		return b, err
	}
	return nil, errors.New("not supported")
}

// FollowLog attempts to follow the logs from systemd-journald. It passes 3 additional flags to journalctl:
//
//	-f --follow      Follow the journal
//	--no-pager       Do not pipe output into a pager
//	--no-tail        Show all lines, even in follow mode
func FollowLog(name string) error {
	if _, err := exec.LookPath("journalctl"); err != nil {
		return errors.New("-f/--follow not supported")
	}

	// Firewalla
	if _, err := os.Stat("/etc/firewalla_release"); err == nil {
		cmd := exec.Command("sudo", "journalctl", "-q", "-b", "-f", "--no-pager", "--no-tail", "-t", name)
		cmd.Stdout = os.Stdout
		return cmd.Run()
	}

	cmd := exec.Command("journalctl", "-q", "-b", "-f", "--no-pager", "--no-tail", "-u", name)
	cmd.Stdout = os.Stdout

	return cmd.Run()
}
