package host

import (
	"bytes"
	"errors"
	"os/exec"
)

func DNS() []string {
	b, err := exec.Command("ipconfig", "getoption", "", "domain_name_server").Output()
	if err != nil {
		return nil
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil
	}
	return []string{string(b)}
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
	b, err := exec.Command("networksetup", "-setdnsservers", networkService, dns).Output()
	if err != nil {
		return errors.New(string(b))
	}
	return nil
}

func listNetworkServices() ([]string, error) {
	b, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, err
	}
	services := []string{}
	for svc := range bytes.SplitSeq(bytes.TrimSpace(b), []byte{'\n'}) {
		if bytes.Contains(svc, []byte{'*'}) {
			// Skip disabled network services.
			continue
		}
		services = append(services, string(svc))
	}
	return services, nil
}
