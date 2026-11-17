package host

import (
	"bytes"
	"fmt"
	"os/exec"
	"time"
)

var macNetworkSetup = networkSetup{path: "networksetup", timeout: 10 * time.Second}

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

func SetDNS(dns string, port uint16) error {
	if port != 53 {
		return fmt.Errorf("set dns: non 53 port not supported on this platform")
	}
	return macNetworkSetup.setDNS(dns)
}

func ResetDNS() error {
	return SetDNS("empty", 53)
}
