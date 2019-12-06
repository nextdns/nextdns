package host

import (
	"bytes"
	"os/exec"
)

func DNS() ([]string, error) {
	b, err := exec.Command("ipconfig", "getoption", "", "domain_name_server").Output()
	if err != nil {
		return nil, err
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, ErrNotFound
	}
	return []string{string(b)}, nil
}

func SetDNS(dns string) error {
	netServices, err := listNetworkServices()
	if err != nil {
		return err
	}
	for _, net := range netServices {
		if err := setDNS(net, dns); err != nil {
			return err
		}
	}
	return nil
}

func ResetDNS() error {
	return SetDNS("empty")
}

func setDNS(networkService, dns string) error {
	return exec.Command("networksetup", "-setdnsservers", networkService, dns).Run()
}

func listNetworkServices() ([]string, error) {
	b, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, err
	}
	services := []string{}
	for _, svc := range bytes.Split(bytes.TrimSpace(b), []byte{'\n'}) {
		if bytes.Contains(svc, []byte{'*'}) {
			// Skip disabled network services.
			continue
		}
		services = append(services, string(svc))
	}
	return services, nil
}
