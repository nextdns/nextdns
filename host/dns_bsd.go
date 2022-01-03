// +build freebsd openbsd netbsd dragonfly

package host

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/net/route"
)

const (
	resolvconfFile       = "/etc/resolvconf.conf"
	resolvconfBackupFile = "/etc/resolvconf.conf.nextdns-bak"
	resolvconfTmpFile    = "/etc/resolvconf.conf.nextdns-tmp"
)

func DNS() (dns []string) {
	return guessDNS(
		func() []string {
			leases, err := filepath.Glob("/var/db/dhclient.leases.*")
			if err != nil {
				return nil
			}
			for _, lease := range leases {
				dns = appendUniq(dns, getDhclientLeaseDNS(lease)...)
			}
			if len(dns) > 0 {
				// Revert order, last is freshier
				for i := 0; i < len(dns)/2; i++ {
					j := len(dns) - 1 - i
					dns[i], dns[j] = dns[j], dns[i]
				}
			}
			return dns
		},
		gatewayDNS,
	)
}

func getDhclientLeaseDNS(lease string) (dns []string) {
	f, err := os.Open(lease)
	if err != nil {
		return
	}
	s := bufio.NewScanner(f)
	const prefix = "  option domain-name-servers"
	for s.Scan() {
		line := s.Text()
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		line = line[len(prefix):]
		line = strings.TrimRight(line, ";")
		if len(line) > 0 {
			for _, ns := range strings.Split(line, ", ") {
				dns = appendUniq(dns, ns)
			}
		}
	}
	return
}

func gatewayDNS() (dns []string) {
	rib, err := route.FetchRIB(0, route.RIBTypeRoute, 0)
	if err != nil {
		return
	}
	messages, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return
	}
	for _, message := range messages {
		message, ok := message.(*route.RouteMessage)
		if !ok {
			continue
		}
		addresses := message.Addrs
		if len(addresses) < 2 {
			continue
		}
		destination, ok := addresses[0].(*route.Inet4Addr)
		if !ok {
			continue
		}
		gateway, ok := addresses[1].(*route.Inet4Addr)
		if !ok {
			continue
		}
		if destination == nil || gateway == nil {
			continue
		}
		if destination.IP == [4]byte{0, 0, 0, 0} {
			if ip := net.IP(gateway.IP[:]).String(); probeDNS(ip) {
				dns = append(dns, ip)
			}
		}
	}
	return
}

func SetDNS(dns string) error {
	if err := setupResolvConf(dns); err != nil {
		return fmt.Errorf("setup resolv.conf: %v", err)
	}
	if err := setupResolvconfConf(); err != nil {
		return fmt.Errorf("setup resolvconf.conf: %v", err)
	}
	return updateResolvconf()
}

func ResetDNS() error {
	if err := os.Rename(resolvBackupFile, resolvFile); err != nil {
		return fmt.Errorf("restore resolv.conf: %v", err)
	}
	if st, err := os.Stat(resolvconfBackupFile); err == nil && st.Size() == 0 {
		// If backup is empty, remove the file
		if err := os.Remove(resolvconfBackupFile); err != nil {
			return fmt.Errorf("remove resolvconf.conf backup: %v", err)
		}
		if err := os.Remove(resolvconfFile); err != nil {
			return fmt.Errorf("restore resolvconf.conf: %v", err)
		}
	} else {
		if err := os.Rename(resolvconfBackupFile, resolvconfFile); err != nil {
			return fmt.Errorf("restore resolvconf.conf: %v", err)
		}
	}
	return updateResolvconf()
}

func updateResolvconf() error {
	return exec.Command("/sbin/resolvconf", "-u").Run()
}

func setupResolvconfConf() error {
	// Make sure we are not already activated.
	backup := true
	if _, err := os.Stat(resolvconfBackupFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %v", resolvconfBackupFile, err)
	} else if err == nil {
		backup = false
	}

	// Write the new resolvconf.conf.
	if err := writeTempResolvconfConf(resolvconfTmpFile); err != nil {
		return fmt.Errorf("write %s: %v", resolvconfTmpFile, err)
	}

	// Backup the current resolvconf.conf.
	if backup {
		if err := os.Rename(resolvconfFile, resolvconfBackupFile); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			// If file did not exist, create an empty file.
			os.WriteFile(resolvconfBackupFile, nil, 0644)
		}
	}

	// Use the new file.
	if err := os.Rename(resolvconfTmpFile, resolvconfFile); err != nil {
		return err
	}
	return nil
}

func writeTempResolvconfConf(tmpPath string) error {
	_ = os.Remove(tmpPath)
	tmp, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer tmp.Close()
	_, err = fmt.Fprintln(tmp, "resolvconf=NO")
	return err
}
