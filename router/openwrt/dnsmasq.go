package openwrt

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	dnsmasqInitScript = "/etc/init.d/dnsmasq"
	// dnsmasqConfigFile is the configuration the init script generates for an
	// instance when it starts it, followed by the name of its uci section.
	dnsmasqConfigFile = "/var/etc/dnsmasq.conf."
)

// dnsmasqEnv is what the conf-dir lookup reads from the system.
type dnsmasqEnv struct {
	uci      func(args ...string) (string, error)
	readFile func(name string) ([]byte, error)
	glob     func(pattern string) ([]string, error)
}

var systemDnsmasqEnv = dnsmasqEnv{uci: uci, readFile: os.ReadFile, glob: filepath.Glob}

// confDir returns the conf-dir of the first dnsmasq instance.
//
// The directory is not read from the running service: we can be started before
// dnsmasq (the package's uci-defaults script starts us from /etc/init.d/boot
// on the first boot after a sysupgrade), and dnsmasq is then not registered
// with procd yet. It is derived the way the init script derives it each time
// it starts dnsmasq: the confdir option, or the default of the init script,
// which is /tmp/dnsmasq.d before 24.10 and /tmp/dnsmasq.<section>.d since.
//
// It is not read from the configuration the init script generated either: we
// restart a started dnsmasq, and the init script then derives the conf-dir
// again from the current settings, which can have changed since. That
// configuration is only used if the default of the init script cannot be read.
//
// Like before, only the first instance is supported.
//
// More info here: https://github.com/openwrt/openwrt/pull/16806
func (e dnsmasqEnv) confDir() (string, error) {
	// -X shows the name of anonymous sections (e.g. dhcp.cfg01411c=dnsmasq).
	out, err := e.uci("-X", "show", "dhcp.@dnsmasq[0]")
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(out, "\n")
	section, typ, ok := strings.Cut(strings.TrimPrefix(line, "dhcp."), "=")
	if !ok || typ != "dnsmasq" || section == "" {
		return "", fmt.Errorf("dnsmasq section not found in %q", line)
	}

	if dir, err := e.uci("get", "dhcp.@dnsmasq[0].confdir"); err == nil {
		dir = firstConfDir(dir)
		if !path.IsAbs(dir) {
			return "", fmt.Errorf("dnsmasq confdir %q is not an absolute path", dir)
		}
		return dir, nil
	} else if !errors.Is(err, errUCIEntryNotFound) {
		return "", err
	}
	script, err := e.readFile(dnsmasqInitScript)
	if err == nil {
		var dir string
		if dir, err = defaultConfDir(string(script), section); err == nil {
			return dir, nil
		}
	}
	// The default cannot be read from the init script. If dnsmasq is started,
	// use the conf-dir it was started with rather than failing.
	if dir, ok := e.generatedConfDir(section); ok {
		return dir, nil
	}
	return "", err
}

// generatedConfDir returns the conf-dir in the configuration the init script
// generated for a section when it started dnsmasq.
func (e dnsmasqEnv) generatedConfDir(section string) (dir string, ok bool) {
	conf, err := e.readFile(dnsmasqConfigFile + section)
	if err != nil {
		return "", false
	}
	for line := range strings.SplitSeq(string(conf), "\n") {
		if dir, ok := strings.CutPrefix(line, "conf-dir="); ok {
			dir = firstConfDir(dir)
			return dir, path.IsAbs(dir)
		}
	}
	return "", false
}

// started reports whether the init script has started dnsmasq since boot. It
// generates the configuration of an instance before starting it.
func (e dnsmasqEnv) started() (bool, error) {
	files, err := e.glob(dnsmasqConfigFile + "*")
	if err != nil {
		return false, err
	}
	for _, file := range files {
		// Section names have no dot: skip the temporary file the init script
		// writes before renaming it.
		if !strings.Contains(strings.TrimPrefix(file, dnsmasqConfigFile), ".") {
			return true, nil
		}
	}
	return false, nil
}

// firstConfDir returns the directory of a conf-dir value, which can be
// followed by file extensions to include or exclude: "dir,*.conf".
func firstConfDir(confDir string) string {
	dir, _, _ := strings.Cut(strings.TrimSpace(confDir), ",")
	return dir
}

// defaultConfDir returns the conf-dir the dnsmasq init script uses for a
// section without a confdir option. It is read from the script, as the
// default depends on the OpenWrt release:
//
//	config_get dnsmasqconfdir "$cfg" confdir "/tmp/dnsmasq.d"
//	config_get dnsmasqconfdir "$cfg" confdir "/tmp/dnsmasq${cfg:+.$cfg}.d"
func defaultConfDir(script, section string) (string, error) {
	for line := range strings.SplitSeq(script, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[0] != "config_get" || fields[1] != "dnsmasqconfdir" || fields[3] != "confdir" {
			continue
		}
		dir := strings.Trim(fields[4], `"'`)
		dir = strings.ReplaceAll(dir, "${cfg:+.$cfg}", "."+section)
		if !path.IsAbs(dir) || strings.Contains(dir, "$") {
			return "", fmt.Errorf("unsupported default dnsmasq conf-dir: %s", fields[4])
		}
		return dir, nil
	}
	return "", fmt.Errorf("default dnsmasq conf-dir not found in %s", dnsmasqInitScript)
}
