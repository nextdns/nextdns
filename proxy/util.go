package proxy

import (
	"net"
	"strings"
)

func addrIP(addr net.Addr) (ip net.IP) {
	// Avoid parsing/alloc when it's an IP already.
	switch addr := addr.(type) {
	case *net.IPAddr:
		ip = addr.IP
	case *net.UDPAddr:
		ip = addr.IP
	case *net.TCPAddr:
		ip = addr.IP
	default:
		host, _, _ := net.SplitHostPort(addr.String())
		ip = net.ParseIP(host)
	}
	return
}

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
