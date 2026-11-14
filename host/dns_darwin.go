package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const networkSetupTimeout = 5 * time.Second

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
	netServices, err := listNetworkServices()
	if err != nil {
		return err
	}
	if len(netServices) == 0 {
		return errors.New("set dns: no network services found")
	}

	var errs []error
	ok := 0
	for _, net := range netServices {
		if err := setDNS(net, dns); err != nil {
			errs = append(errs, err)
			continue
		}
		ok++
	}
	if ok == 0 {
		return fmt.Errorf("set dns: failed on all network services: %w", errors.Join(errs...))
	}
	// Some virtual/VPN services may time out or refuse changes; keep going so
	// activation does not hang forever on those entries (see #1107).
	return nil
}

func ResetDNS() error {
	return SetDNS("empty", 53)
}

func setDNS(networkService, dns string) error {
	ctx, cancel := context.WithTimeout(context.Background(), networkSetupTimeout)
	defer cancel()
	b, err := exec.CommandContext(ctx, "networksetup", "-setdnsservers", networkService, dns).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(b))
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("networksetup -setdnsservers %s: timed out after %s", networkService, networkSetupTimeout)
		}
		if msg == "" {
			return fmt.Errorf("networksetup -setdnsservers %s: %v", networkService, err)
		}
		return fmt.Errorf("networksetup -setdnsservers %s: %s", networkService, msg)
	}
	return nil
}

func listNetworkServices() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), networkSetupTimeout)
	defer cancel()
	b, err := exec.CommandContext(ctx, "networksetup", "-listallnetworkservices").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(b))
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("networksetup -listallnetworkservices: timed out after %s", networkSetupTimeout)
		}
		if msg == "" {
			return nil, fmt.Errorf("networksetup -listallnetworkservices: %v", err)
		}
		return nil, fmt.Errorf("networksetup -listallnetworkservices: %s", msg)
	}
	services := []string{}
	for svc := range bytes.SplitSeq(bytes.TrimSpace(b), []byte{'\n'}) {
		if bytes.Contains(svc, []byte{'*'}) {
			// Skip disabled network services and the header line that
			// mentions the asterisk marker.
			continue
		}
		if len(bytes.TrimSpace(svc)) == 0 {
			continue
		}
		services = append(services, string(svc))
	}
	return services, nil
}
