// +build windows

package arp

// Windows arp table reader added by Claudio Matsuoka.
// Tested only in Windows 8.1, hopefully the arp command output format
// is the same in other Windows versions.

import (
	"net"
	"os/exec"
	"strings"
)

func Get() (Table, error) {
	data, err := exec.Command("arp", "-a").Output()
	if err != nil {
		return nil, err
	}

	var t Table
	skipNext := false
	for _, line := range strings.Split(string(data), "\n") {
		// skip empty lines
		if len(line) <= 0 {
			continue
		}
		// skip Interface: lines
		if line[0] != ' ' {
			skipNext = true
			continue
		}
		// skip column headers
		if skipNext {
			skipNext = false
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		t = append(t, Entry{
			IP:  net.ParseIP(fields[0]),
			MAC: parseMAC(fields[1]),
		})
	}

	return t, nil
}
