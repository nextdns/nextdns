package host

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/nextdns/nextdns/internal/resolved"
)

func DNS() []string {
	return guessDNS(
		nmcliGet,
		func() []string {
			var dns []string
			ifaces, err := net.Interfaces()
			if err != nil {
				return nil
			}
			for _, iface := range ifaces {
				if iface.Flags&net.FlagUp == 0 {
					continue
				}
				if iface.Flags&net.FlagLoopback != 0 {
					continue
				}
				dns = append(dns, dhcpcdGet(iface.Name)...)
			}
			return dns
		},
		func() []string {
			var dns []string
			const leaseDir = "/run/systemd/netif/leases"
			if leases, err := os.ReadDir(leaseDir); err == nil {
				for _, lease := range leases {
					if lease.IsDir() || strings.HasPrefix(lease.Name(), ".") {
						continue
					}
					dns = append(dns, systemdLeaseDNSGet(filepath.Join(leaseDir, lease.Name()))...)
				}
			}
			return dns
		},
		gatewayDNS,
		gatewayDNS6,
	)
}

func SetDNS(dns string, port uint16) error {
	if port == 0 {
		port = 53
	}
	var resolvedErr error
	if resolved.Available() {
		fmt.Printf("systemd-resolved is available, setting DNS to %s:%d\n", dns, port)
		if err := resolved.SetDNS(dns, port); err == nil {
			return nil
		} else if port != 53 {
			return err
		} else {
			resolvedErr = err
		}
	}
	if err := setDNSResolvConf(dns, port); err != nil {
		if resolvedErr != nil {
			return fmt.Errorf("systemd-resolved activation failed: %v; resolv.conf fallback failed: %w", resolvedErr, err)
		}
		return err
	}
	return nil
}

func setDNSResolvConf(dns string, port uint16) error {
	if port != 53 {
		return fmt.Errorf("setup resolv.conf: non 53 port not supported without systemd-resolved D-Bus")
	}
	if err := setupResolvConf(dns); err != nil {
		return fmt.Errorf("setup resolv.conf: %v", err)
	}
	if err := disableNetworkManagerResolver(); err != nil {
		return fmt.Errorf("NetworkManager resolver management: %v", err)
	}
	return nil
}

func ResetDNS() error {
	if resolved.StateExists() {
		return resolved.ResetDNS()
	}
	return resetDNSResolvConf()
}

func resetDNSResolvConf() error {
	if err := os.Rename(resolvBackupFile, resolvFile); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("restore resolv.conf: %v", err)
	}
	if err := restoreNetworkManagerResolver(); err != nil {
		return fmt.Errorf("NetworkManager resolver management: %v", err)
	}
	return nil
}

func nmcliGet() (dns []string) {
	b, err := exec.Command("nmcli", "dev", "show").Output()
	if err != nil {
		return dns
	}
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
	return dns
}

func dhcpcdGet(iface string) (dns []string) {
	b, err := exec.Command("dhcpcd", "-U", iface).Output()
	if err != nil {
		return dns
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "domain_name_servers=") {
			line, err := strconv.Unquote(line[21:])
			if err != nil {
				continue
			}
			dns = append(dns, strings.Split(line, " ")...)
		}
	}
	return dns
}

func systemdLeaseDNSGet(file string) (dns []string) {
	f, err := os.Open(file)
	if err != nil {
		return
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "DNS=") {
			dns = append(dns, strings.TrimPrefix(line, "DNS="))
		}
	}
	return
}

func gatewayDNS() []string {
	return gatewayDNSCommon("/proc/net/route", 4)
}

func gatewayDNS6() []string {
	return gatewayDNSCommon("/proc/net/ipv6_route", 16)
}

func gatewayDNSCommon(file string, size int) (dns []string) {
	f, err := os.Open(file)
	hexSize := size << 1
	if err != nil {
		return dns
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	ip := make([]byte, size) // init empty IP also used to find default gateway
	for s.Scan() {
		fields := bytes.Fields(s.Bytes())
		if len(fields) < 3 || len(fields[1]) != hexSize || len(fields[2]) != hexSize {
			continue
		}
		if !bytes.Equal(fields[1], ip) {
			continue
		}
		_, err := hex.Decode(ip, fields[2])
		if err != nil {
			return dns
		}
		if ip := net.IP(ip).String(); probeDNS(ip) {
			dns = append(dns, ip)
		}
	}
	return dns
}

var networkManagerFile = "/etc/NetworkManager/conf.d/nextdns.conf"

func disableNetworkManagerResolver() error {
	confDir := filepath.Dir(networkManagerFile)
	if st, err := os.Stat(confDir); err != nil {
		if os.IsNotExist(err) {
			// NetworkManager does not seem to exist on this system, just ignore.
			return nil
		}
		return err
	} else if !st.IsDir() {
		return fmt.Errorf("%s: is not a directory", confDir)
	}

	// Disable resolv.conf management by NetworkManager
	if err := os.WriteFile(networkManagerFile, []byte("[main]\ndns=none\n"), 0644); err != nil {
		// Some systems mount /etc on constrained or read-only storage.
		// Keep activation working even if this optional override cannot be persisted.
		if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EROFS) {
			return nil
		}
		return err
	}

	return reloadNetworkManagerIfActive()
}

func restoreNetworkManagerResolver() error {
	if _, err := os.Stat(networkManagerFile); err != nil {
		return nil
	}
	if err := os.Remove(networkManagerFile); err != nil {
		return err
	}
	return reloadNetworkManagerIfActive()
}

func reloadNetworkManagerIfActive() error {
	// On many systems NetworkManager may be installed but not the active
	// network manager (or not running). Treat that as non-fatal.
	if err := exec.Command("systemctl", "is-active", "--quiet", "NetworkManager").Run(); err != nil {
		return nil
	}
	out, err := exec.Command("systemctl", "reload", "NetworkManager").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("reload NetworkManager: %v: %s", err, msg)
		}
		return fmt.Errorf("reload NetworkManager: %v", err)
	}
	return nil
}
