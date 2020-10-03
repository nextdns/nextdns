// +build !linux,!windows

package ndp

import (
	"net"
	"os/exec"
	"strings"
)

func Get() (Table, error) {
	data, err := exec.Command("ndp", "-an").Output()
	if err != nil {
		return nil, err
	}

	var t Table
	header := true
	for _, line := range strings.Split(string(data), "\n") {
		if header {
			header = false
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		if mac := parseMAC(fields[1]); mac != nil {
			ip := fields[0]
			if idx := strings.IndexByte(ip, '%'); idx != -1 {
				ip = ip[:idx]
			}
			t = append(t, Entry{
				IP:  net.ParseIP(ip),
				MAC: mac,
			})
		}
	}

	return t, nil
}
