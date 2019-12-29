// +build freebsd openbsd netbsd dragonfly

package host

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	resolvconfFile       = "/etc/resolvconf.conf"
	resolvconfBackupFile = "/etc/resolvconf.conf.nextdns-bak"
	resolvconfTmpFile    = "/etc/resolvconf.conf.nextdns-tmp"
)

func DNS() ([]string, error) {
	leases, err := filepath.Glob("/var/db/dhclient.leases.*")
	if err != nil {
		return nil, err
	}
	var allDNS []string
	for _, lease := range leases {
		dns, err := getDhclientLeaseDNS(lease)
		if err != nil {
			return nil, err
		}
		allDNS = appendUniq(allDNS, dns...)
	}
	if len(allDNS) > 0 {
		// Revert order, last is freshier
		for i := 0; i < len(allDNS)/2; i++ {
			j := len(allDNS) - 1 - i
			allDNS[i], allDNS[j] = allDNS[j], allDNS[i]
		}
		return allDNS, nil
	}
	return nil, ErrNotFound
}

func appendUniq(set []string, adds ...string) []string {
	for i := range adds {
		found := false
		for j := range set {
			if adds[i] == set[j] {
				found = true
				break
			}
		}
		if !found {
			set = append(set, adds[i])
		}
	}
	return set
}

func getDhclientLeaseDNS(lease string) (dns []string, err error) {
	f, err := os.Open(lease)
	if err != nil {
		return nil, err
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
	if err := s.Err(); err != nil {
		return nil, err
	}
	return dns, err
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
			ioutil.WriteFile(resolvconfBackupFile, nil, 0644)
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
