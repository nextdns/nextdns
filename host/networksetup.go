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

// networkSetup applies DNS settings with macOS networksetup. It is not build
// tagged so it can be tested with a stub command on any Unix.
type networkSetup struct {
	path    string
	timeout time.Duration // per networksetup call
}

// setDNS sets dns on every enabled network service. A failing or stalled
// service does not prevent the others from being updated, but any failure is
// returned.
func (n networkSetup) setDNS(dns string) error {
	services, err := n.listNetworkServices()
	if err != nil {
		return err
	}
	var errs []error
	for _, svc := range services {
		if _, err := n.run("-setdnsservers", svc, dns); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (n networkSetup) listNetworkServices() ([]string, error) {
	b, err := n.run("-listallnetworkservices")
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

func (n networkSetup) run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, n.path, args...)
	// Do not wait for output still held open by children of a killed command.
	cmd.WaitDelay = time.Second
	b, err := cmd.Output()
	if err == nil {
		return b, nil
	}
	name := "networksetup " + strings.Join(args, " ")
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s: timed out after %v", name, n.timeout)
	}
	if msg := strings.TrimSpace(string(b)); msg != "" {
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	return nil, fmt.Errorf("%s: %v", name, err)
}
