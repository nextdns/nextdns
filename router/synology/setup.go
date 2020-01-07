package synology

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/nextdns/nextdns/config"
)

type Router struct {
	DHCPVendorConf string
}

func New() (*Router, bool) {
	if b, err := exec.Command("uname", "-u").Output(); err != nil ||
		!strings.HasPrefix(string(b), "synology") {
		return nil, false
	}
	return &Router{
		DHCPVendorConf: "/etc/dhcpd/dhcpd-vendor.conf",
	}, true
}

func (r *Router) Configure(c *config.Config) error {
	c.Listen = ":53"
	if err := ioutil.WriteFile(r.DHCPVendorConf, []byte("port 0\n"), 0644); err != nil {
		return err
	}
	// Restart dnsmasq service to apply changes.
	if err := exec.Command("/etc/rc.network", "nat-restart-dhcp").Run(); err != nil {
		return fmt.Errorf("dnsmasq restart: %v", err)
	}
	return nil
}

func (r *Router) Setup() error {
	return nil
}

func (r *Router) Restore() error {
	if os.Remove(r.DHCPVendorConf) == nil {
		// Restart dnsmasq service to apply changes.
		if err := exec.Command("/etc/rc.network", "nat-restart-dhcp").Run(); err != nil {
			return fmt.Errorf("dnsmasq restart: %v", err)
		}
	}
	return nil
}
