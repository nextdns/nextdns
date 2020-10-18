package ubios

import (
	"os"
	"os/exec"
	"strings"

	"github.com/nextdns/nextdns/config"
)

type Router struct {
}

func New() (*Router, bool) {
	if st, _ := os.Stat("/etc/unifi-os"); st == nil || !st.IsDir() {
		return nil, false
	}
	return &Router{}, true
}

func (r *Router) Configure(c *config.Config) error {
	c.Listens = []string{"localhost:5553"}
	c.DiscoveryDNS = "127.0.0.1"
	return nil
}

func (r *Router) Setup() error {
	if err := run("sysctl -w net.ipv4.conf.all.route_localnet=1"); err != nil {
		return err
	}
	if err := run("iptables -t nat -N NEXTDNS"); err != nil {
		return err
	}
	if err := run("iptables -t nat -I PREROUTING 1 ! -d 127.0.0.0/8 -j NEXTDNS"); err != nil {
		return err
	}
	if err := run("iptables -t nat -A NEXTDNS -p udp -m udp --dport 53 -j DNAT --to-destination 127.0.0.1:5553"); err != nil {
		return err
	}
	if err := run("iptables -t nat -A NEXTDNS -p tcp -m tcp --dport 53 -j DNAT --to-destination 127.0.0.1:5553"); err != nil {
		return err
	}
	return nil
}

func (r *Router) Restore() error {
	if err := run("iptables -t nat -D PREROUTING ! -d 127.0.0.0/8 -j NEXTDNS"); err != nil {
		return err
	}
	if err := run("iptables -t nat -F NEXTDNS"); err != nil {
		return err
	}
	if err := run("iptables -t nat -X NEXTDNS"); err != nil {
		return err
	}
	return nil
}

func run(cmd string) error {
	args := append([]string{"-oStrictHostKeyChecking=no", "127.0.0.1"}, strings.Fields(cmd)...)
	return exec.Command("ssh", args...).Run()
}
