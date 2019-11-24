package dnsconf

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

func Get() ([]string, error) {
	dns, err := nmcliGet()
	if err == nil {
		return dns, nil
	}
	ifaces, err := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		dns, err := dhcpcdGet(iface.Name)
		if err == nil {
			return dns, nil
		}
	}
	return nil, ErrNotFound
}

func nmcliGet() ([]string, error) {
	b, err := exec.Command("nmcli", "dev", "show").Output()
	if err != nil {
		return nil, err
	}
	var dns []string
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "IP4.DNS") {
			kv := strings.SplitN(line, ":", 2)
			if len(kv) == 2 {
				dns = append(dns, strings.TrimSpace(kv[1]))
			}
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(dns) > 0 {
		return dns, nil
	}
	return nil, ErrNotFound
}

func dhcpcdGet(iface string) ([]string, error) {
	b, err := exec.Command("dhcpcd", "-U", iface).Output()
	if err != nil {
		return nil, err
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "domain_name_servers=") {
			line, err := strconv.Unquote(line[21:])
			if err != nil {
				return nil, fmt.Errorf("unquote: %v", err)
			}
			return strings.Split(line, " "), nil
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}
