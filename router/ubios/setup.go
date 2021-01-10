package ubios

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nextdns/nextdns/config"
)

type Router struct {
}

var privateNets = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

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
	for _, net := range privateNets {
		if err := run(fmt.Sprintf("iptables -t nat -I PREROUTING 1 -d %s -j NEXTDNS", net)); err != nil {
			return err
		}
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
	for _, net := range privateNets {
		if err := run(fmt.Sprintf("iptables -t nat -D PREROUTING -d %s -j NEXTDNS", net)); err != nil {
			return err
		}
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
