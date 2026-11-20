package openwrt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"os/exec"
	"strings"
)

func dnsmaskConfDir() string {
	// Search for a valid dnsmasq configuration directory as starting with
	// 24.10, several instances of dnsmasq can be run with different
	// configurations.
	//
	// This implementation will probably not work with setups when dnsmasq
	// actually has multiple instances with different configurations as we only
	// support a single instance and it is not clear how to determine which one
	// we should alter. Neither there seem to be a way to determine which one of
	// those configuration are index 0 in `uci`.
	//
	// Hopefully a better solution will be implemented in OpenWRT in the future.
	//
	// More info here: https://github.com/openwrt/openwrt/pull/16806
	out, err := exec.Command("ubus", "call", "service", "list").Output()
	if err != nil {
		return "/tmp/dnsmasq.d"
	}
	return parseUbusDnsmasqConfDir(out)
}

// dnsmasqConfDirFromService reports only a directory actually advertised by
// dnsmasq. The legacy fallback is not safe when claiming port 53 at boot.
func dnsmasqConfDirFromService() (string, bool) {
	out, err := exec.Command("ubus", "call", "service", "list").Output()
	if err != nil { return "", false }
	return parseUbusDNSMasqDir(out)
}

func waitForDNSMasqConfDir(timeout time.Duration, lookup func() (string, bool)) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if dir, ok := lookup(); ok {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				return filepath.Join(dir, "nextdns.conf"), nil
			}
		}
		if !time.Now().Before(deadline) { return "", fmt.Errorf("dnsmasq runtime configuration directory not ready after %s", timeout) }
		time.Sleep(250 * time.Millisecond)
	}
}

func parseUbusDnsmasqConfDir(out []byte) string {
	if dir, ok := parseUbusDNSMasqDir(out); ok { return dir }
	return "/tmp/dnsmasq.d"
}

func parseUbusDNSMasqDir(out []byte) (string, bool) {
	var conf struct {
		DNSMasq struct {
			Instances map[string]struct {
				Mount map[string]string
			}
		}
	}
	if json.Unmarshal(out, &conf) != nil {
		return "", false
	}
	for _, inst := range conf.DNSMasq.Instances {
		for dir := range inst.Mount {
			if strings.HasSuffix(dir, ".d") && strings.Contains(dir, "dnsmasq") {
				return dir, true
			}
		}
	}
	return "", false
}
