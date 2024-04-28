//go:build linux
// +build linux

package ndp

import (
	"bufio"
	"net"
	"os/exec"
	"strings"
)

func Get() (t Table, err error) {
	cmd := exec.Command("ip", "-6", "neigh")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return t, err
	}

	if err := cmd.Start(); err != nil {
		return t, err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 5 {
			ip := net.ParseIP(parts[0])
			mac, _ := net.ParseMAC(parts[4])
			if ip != nil && mac != nil {
				t = append(t, Entry{
					IP:  ip,
					MAC: mac,
				})
			}
		}
	}

	return t, cmd.Wait()
}
