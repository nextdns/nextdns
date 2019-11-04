// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	resolvBackupFile   = "/etc/resolv.conf.nextdns-bak"
	networkManagerFile = "/etc/NetworkManager/conf.d/nextdns.conf"
)

func activate(string) error {
	if err := setupResolvConf(); err != nil {
		return fmt.Errorf("setup resolv.conf: %v", err)
	}
	if err := disableNetworkManagerResolver(); err != nil {
		return fmt.Errorf("NetworkManager resolver management: %v", err)
	}
	return nil
}

func deactivate(string) error {
	if err := os.Rename(resolvBackupFile, "/etc/resolv.conf"); err != nil {
		return fmt.Errorf("restore resolv.conf: %v", err)
	}
	if err := restoreNetworkManagerResolver(); err != nil {
		return fmt.Errorf("NetworkManager resolver management: %v", err)
	}
	return nil
}

func setupResolvConf() error {
	tmpPath := "/etc/resolv.conf.nextdns-tmp"

	// Make sure we are not already activated.
	if _, err := os.Stat(resolvBackupFile); err == nil || !os.IsNotExist(err) {
		if err == nil {
			err = errors.New("file exists")
		}
		return fmt.Errorf("%s: %v", resolvBackupFile, err)
	}

	// Write the new resolv.conf.
	if err := writeTempResolvConf(tmpPath); err != nil {
		return fmt.Errorf("write %s: %v", tmpPath, err)
	}

	// Backup the current resolv.conf.
	if err := os.Rename("/etc/resolv.conf", resolvBackupFile); err != nil {
		return err
	}

	// Use the new file.
	if err := os.Rename(tmpPath, "/etc/resolv.conf"); err != nil {
		return err
	}
	return nil
}

func writeTempResolvConf(tmpPath string) error {
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
	fmt.Fprintln(tmp, "nameserver 127.0.0.1")
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
