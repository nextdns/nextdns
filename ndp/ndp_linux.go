// +build linux

package ndp

import (
	"net"
	"os/exec"
	"strings"
)

func Get() (Table, error) {
	data, err := exec.Command("ip", "-6", "neighbor", "show").Output()
	if err != nil {
		return nil, err
	}

	var t Table
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		t = append(t, Entry{
			IP:  net.ParseIP(fields[0]),
			MAC: parseMAC(fields[4]),
		})
	}

	return t, nil
}
