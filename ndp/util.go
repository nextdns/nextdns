// +build !linux

package ndp

import (
	"net"
	"strings"
)

func parseMAC(s string) net.HardwareAddr {
	if len(s) < 17 {
		comp := strings.Split(s, ":")
		if len(comp) != 6 {
			return nil
		}
		for i, c := range comp {
			if len(c) == 1 {
				comp[i] = "0" + c
			}
		}
		s = strings.Join(comp, ":")
	}
	mac, _ := net.ParseMAC(s)
	return mac
}
