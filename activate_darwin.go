// +build darwin

package main

import (
	"bytes"
	"os/exec"
)

func activate(string) error {
	netServices, err := listNetworkServices()
	if err != nil {
		return err
	}
	for _, net := range netServices {
		if err := setDNS(net, "127.0.0.1"); err != nil {
			return err
		}
	}
	return nil
}

func deactivate(string) error {
	netServices, err := listNetworkServices()
	if err != nil {
		return err
	}
	for _, net := range netServices {
		if err := resetDNS(net); err != nil {
			return err
		}
	}
	return nil
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

func setDNS(networkService, dns string) error {
	return exec.Command("networksetup", "-setdnsservers", networkService, dns).Run()
}

func resetDNS(networkService string) error {
	return setDNS(networkService, "empty")
}
