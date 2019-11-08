// +build !linux,!windows

// only tested on OSX
// decided to go with exec.Command after I couldn't figure
// out how to extract the arp cache out of the kernel with
// golang's syscall or Sysctl()
//
// ... Help appreciated :)

package arp

import (
	"net"
	"os/exec"
	"strings"
)

func Get() (Table, error) {
	data, err := exec.Command("arp", "-an").Output()
	if err != nil {
		return nil, err
	}

	var t Table
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		// strip brackets around IP
		ip := strings.Replace(fields[1], "(", "", -1)
		ip = strings.Replace(ip, ")", "", -1)

		t = append(t, Entry{
			IP:  net.ParseIP(ip),
			MAC: parseMAC(fields[3]),
		})
	}

	return t, nil
}
