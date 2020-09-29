// +build windows

package ndp

import (
	"net"
	"os/exec"
	"strings"
)

func Get() (Table, error) {
	data, err := exec.Command("netsh", "interface", "ipv6", "show", "neighbors").Output()
	if err != nil {
		return nil, err
	}

	var t Table
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		if mac := parseMAC(strings.ReplaceAll(fields[1], "-", ":")); mac != nil {
			t = append(t, Entry{
				IP:  net.ParseIP(fields[0]),
				MAC: mac,
			})
		}
	}

	return t, nil
}
