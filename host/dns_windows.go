package host

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func DNS() []string {
	return nil
}

func SetDNS(dns string, port uint16) error {
	if port != 53 {
		return fmt.Errorf("set dns: non 53 port not supported on this platform")
	}
	ifaces, err := getInterfaces()
	if err != nil {
		return err
	}
	for _, idx := range ifaces {
		if e := setDNS(idx, dns); err == nil {
			err = e
		}
	}
	return err
}

func ResetDNS() error {
	ifaces, err := getInterfaces()
	if err != nil {
		return err
	}
	for _, idx := range ifaces {
		if e := resetDNS(idx); err == nil {
			err = e
		}
	}
	return nil
}

func setDNS(idx, dns string) error {
	err := netsh("interface", "ipv4", "set", "dnsserver", idx, "static", dns, "primary")
	netsh("interface", "ipv6", "set", "dnsserver", idx, "static", "::1", "primary") // TODO: properly handle v6
	if err != nil {
		err = fmt.Errorf("set %s %s: %v", idx, dns, err)
	}
	return err
}

func resetDNS(idx string) error {
	err := netsh("interface", "ipv4", "set", "dnsserver", idx, "dhcp")
	netsh("interface", "ipv6", "set", "dnsserver", idx, "dhcp")
	if err != nil {
		err = fmt.Errorf("reset dns %s: %v", idx, err)
	}
	return err
}

func netsh(args ...string) error {
	b, err := exec.Command("netsh", args...).Output()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(b))
	}
	return nil
}

func getInterfaces() (ifaces []string, err error) {
	b, err := exec.Command("netsh", "interface", "ipv4", "show", "interfaces").Output()
	if err != nil {
		return nil, err
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		line := s.Text()
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if _, err := strconv.ParseUint(fields[0], 10, 32); err != nil {
			continue
		}
		ifaces = append(ifaces, fields[0])
	}
	return ifaces, nil
}
