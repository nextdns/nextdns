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

// confDir returns the conf-dir of the first dnsmasq instance, and whether the
// init script of dnsmasq has started it since boot.
//
// The directory is not read from the running service: we can be started before
// dnsmasq (the package's uci-defaults script starts us from /etc/init.d/boot
// on the first boot after a sysupgrade), and dnsmasq is then not registered
// with procd yet. Once the init script has run, the conf-dir is read from the
// configuration it generated. Before that, it is derived the way the init
// script will derive it: the confdir option, or the default of the init
// script, which is /tmp/dnsmasq.d before 24.10 and /tmp/dnsmasq.<section>.d
// since.
//
// Like before, only the first instance is supported.
//
// More info here: https://github.com/openwrt/openwrt/pull/16806
func (e dnsmasqEnv) confDir() (dir string, started bool, err error) {
	// -X shows the name of anonymous sections (e.g. dhcp.cfg01411c=dnsmasq).
	out, err := e.uci("-X", "show", "dhcp.@dnsmasq[0]")
	if err != nil {
		return "", false, err
	}
	line, _, _ := strings.Cut(out, "\n")
	section, typ, ok := strings.Cut(strings.TrimPrefix(line, "dhcp."), "=")
	if !ok || typ != "dnsmasq" || section == "" {
		return "", false, fmt.Errorf("dnsmasq section not found in %q", line)
	}

	generated, err := e.generatedConfig(section)
	if err != nil {
		return "", true, err
	}
	if generated != "" {
		conf, err := e.readFile(generated)
		if err != nil {
			return "", true, err
		}
		for line := range strings.SplitSeq(string(conf), "\n") {
			if dir, ok := strings.CutPrefix(line, "conf-dir="); ok {
				if dir = firstConfDir(dir); !path.IsAbs(dir) {
					return "", true, fmt.Errorf("dnsmasq conf-dir %q is not an absolute path", dir)
				}
				return dir, true, nil
			}
		}
		return "", true, errors.New("dnsmasq is started without a conf-dir")
	}

	if dir, err := e.uci("get", "dhcp.@dnsmasq[0].confdir"); err == nil {
		dir = firstConfDir(dir)
		if !path.IsAbs(dir) {
			return "", false, fmt.Errorf("dnsmasq confdir %q is not an absolute path", dir)
		}
		return dir, false, nil
	} else if !errors.Is(err, errUCIEntryNotFound) {
		return "", false, err
	}
	script, err := e.readFile(dnsmasqInitScript)
	if err != nil {
		return "", false, err
	}
	dir, err = defaultConfDir(string(script), section)
	return dir, false, err
}

// generatedConfig returns the configuration the init script generated for
// dnsmasq, or an empty string if it has not started it since boot.
//
// It is the one of the first section, unless dnsmasq is started for a single
// other section: the first one can be disabled, or the sections can have been
// reordered since dnsmasq started, which renames anonymous ones.
func (e dnsmasqEnv) generatedConfig(section string) (string, error) {
	files, err := e.glob(dnsmasqConfigFile + "*")
	if err != nil {
		return "", err
	}
	var generated []string
	for _, file := range files {
		name := strings.TrimPrefix(file, dnsmasqConfigFile)
		if name == section {
			return file, nil
		}
		// Section names have no dot: skip the temporary file the init script
		// writes before renaming it.
		if !strings.Contains(name, ".") {
			generated = append(generated, file)
		}
	}
	switch len(generated) {
	case 0:
		return "", nil
	case 1:
		return generated[0], nil
	}
	return "", fmt.Errorf("dnsmasq is started for several sections, but not for the first one (%s)", section)
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
