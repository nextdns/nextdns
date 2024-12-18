package openwrt

import (
	"encoding/json"
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

func parseUbusDnsmasqConfDir(out []byte) string {
	var conf struct {
		DNSMasq struct {
			Instances map[string]struct {
				Mount map[string]string
			}
		}
	}
	if json.Unmarshal(out, &conf) != nil {
		return "/tmp/dnsmasq.d"
	}
	for _, inst := range conf.DNSMasq.Instances {
		for dir := range inst.Mount {
			if strings.HasSuffix(dir, ".d") && strings.Contains(dir, "dnsmasq") {
				return dir
			}
		}
	}
	return "/tmp/dnsmasq.d"
}
