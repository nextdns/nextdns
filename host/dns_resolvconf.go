// +build linux freebsd openbsd netbsd dragonfly

package host

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	resolvFile       = "/etc/resolv.conf"
	resolvBackupFile = "/etc/resolv.conf.nextdns-bak"
	resolvTmpFile    = "/etc/resolv.conf.nextdns-tmp"
)

func setupResolvConf(dns string) error {
	// Make sure we are not already activated.
	backup := true
	if _, err := os.Stat(resolvBackupFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %v", resolvBackupFile, err)
	} else if err == nil {
		backup = false
	}

	// Write the new resolv.conf.
	if err := writeTempResolvConf(resolvTmpFile, dns); err != nil {
		return fmt.Errorf("write %s: %v", resolvTmpFile, err)
	}

	// Backup the current resolv.conf.
	if backup {
		if err := os.Rename(resolvFile, resolvBackupFile); err != nil {
			return err
		}
	}

	// Use the new file.
	if err := os.Rename(resolvTmpFile, resolvFile); err != nil {
		return err
	}
	return nil
}

func writeTempResolvConf(tmpPath, dns string) error {
	resolv, err := os.Open(resolvFile)
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
	fmt.Fprintf(tmp, "nameserver %s\n", dns)
	if err := s.Err(); err != nil {
		return err
	}
	return nil
}
