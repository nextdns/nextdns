package host

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func DNS() ([]string, error) {
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

var (
	resolvBackupFile   = "/etc/resolv.conf.nextdns-bak"
	networkManagerFile = "/etc/NetworkManager/conf.d/nextdns.conf"
)

func SetDNS(dns string) error {
	if err := setupResolvConf(); err != nil {
		return fmt.Errorf("setup resolv.conf: %v", err)
	}
	if err := disableNetworkManagerResolver(); err != nil {
		return fmt.Errorf("NetworkManager resolver management: %v", err)
	}
	return nil
}

func ResetDNS() error {
	if err := os.Rename(resolvBackupFile, "/etc/resolv.conf"); err != nil {
		return fmt.Errorf("restore resolv.conf: %v", err)
	}
	if err := restoreNetworkManagerResolver(); err != nil {
		return fmt.Errorf("NetworkManager resolver management: %v", err)
	}
	return nil
}

func setupResolvConf(dns string) error {
	tmpPath := "/etc/resolv.conf.nextdns-tmp"

	// Make sure we are not already activated.
	backup := true
	if _, err := os.Stat(resolvBackupFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %v", resolvBackupFile, err)
	} else if err == nil {
		backup = false
	}

	// Write the new resolv.conf.
	if err := writeTempResolvConf(tmpPath, dns); err != nil {
		return fmt.Errorf("write %s: %v", tmpPath, err)
	}

	// Backup the current resolv.conf.
	if backup {
		if err := os.Rename("/etc/resolv.conf", resolvBackupFile); err != nil {
			return err
		}
	}

	// Use the new file.
	if err := os.Rename(tmpPath, "/etc/resolv.conf"); err != nil {
		return err
	}
	return nil
}

func writeTempResolvConf(tmpPath, dns string) error {
	resolv, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return err
	}
	defer resolv.Close()

	_ = os.Remove(tmpPath)
	tmp, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer tmp.Close()

	s := bufio.NewScanner(resolv)
	fmt.Fprintln(tmp, "# This file is managed by nextdns.")
	fmt.Fprintln(tmp, "#")
	fmt.Fprintln(tmp, "# Run \"nextdns deactivate\" to restore previous configuration.")
	fmt.Fprintln(tmp, "")
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" ||
			strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "nameserver ") {
			continue
		}
		fmt.Fprintln(tmp, line)
	}
	fmt.Fprintf(tmp, "nameserver %s", dns)
	if err := s.Err(); err != nil {
		return err
	}
	return nil
}

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
	if err := ioutil.WriteFile(networkManagerFile, []byte("[main]\ndns=none\n"), 0644); err != nil {
		return err
	}

	// Restart network manager
	return exec.Command("systemctl", "try-restart", "NetworkManager").Run()
}

func restoreNetworkManagerResolver() error {
	if _, err := os.Stat(networkManagerFile); err != nil {
		return nil
	}
	if err := os.Remove(networkManagerFile); err != nil {
		return err
	}
	return exec.Command("systemctl", "try-restart", "NetworkManager").Run()
}
