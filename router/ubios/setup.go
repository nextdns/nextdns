package ubios

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/nextdns/nextdns/config"
)

type Router struct {
	LANIPv6   string
	UsePodman bool
}

func New() (*Router, bool) {
	if st, _ := os.Stat("/data/unifi"); st == nil || !st.IsDir() {
		return nil, false
	}
	ipv6, _ := getIface6GlobalIP("br0")
	usePodman, _ := isContainerized()
	return &Router{
		LANIPv6:   ipv6,
		UsePodman: usePodman,
	}, true
}

func isContainerized() (bool, error) {
	f, err := os.Open("/proc/1/cgroup")
	if err != nil {
		return false, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		flds := strings.Split(s.Text(), ":")
		if len(flds) != 3 {
			continue
		}
		if flds[2] != "/" && flds[2] != "/init.scope" {
			return true, nil
		}
	}
	return false, nil
}

func getIface6GlobalIP(iface string) (string, error) {
	f, err := os.Open("/proc/net/if_inet6")
	if err != nil {
		return "", err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		flds := strings.Fields(s.Text())
		if len(flds) != 6 {
			continue
		}
		if flds[5] == iface && flds[3] == "00" {
			return formatIPv6(flds[0]), nil
		}
	}
	return "", nil
}

func getInterfaceNames(pattern string) ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{}, err
	}
	names := make([]string, 0)
	for _, iface := range ifaces {
		if strings.HasPrefix(iface.Name, pattern) {
			names = append(names, iface.Name)
		}
	}
	return names, nil
}

func formatIPv6(ipv6 string) string {
	ipv6b := []byte(ipv6)
	ipv6out := make([]byte, 0, 39)
	for len(ipv6b) > 0 {
		if len(ipv6out) > 0 {
			ipv6out = append(ipv6out, ':')
		}
		ipv6out = append(ipv6out, ipv6b[:4]...)
		ipv6b = ipv6b[4:]

	}
	return net.ParseIP(string(ipv6out)).String()
}

func (r *Router) Configure(c *config.Config) error {
	c.Listens = []string{"localhost:5553"}
	if r.LANIPv6 != "" {
		c.Listens = append(c.Listens, net.JoinHostPort(r.LANIPv6, "5553"))
	}
	c.DiscoveryDNS = "127.0.0.1"
	return nil
}

func (r *Router) Setup() error {
	if err := r.run("sysctl -w net.ipv4.conf.all.route_localnet=1"); err != nil {
		return err
	}
	for _, iptables := range []string{"iptables", "ip6tables"} {
		var match, redirect string
		switch iptables {
		case "iptables":
			match = "-m addrtype --dst-type LOCAL"
			redirect = "-j DNAT --to-destination 127.0.0.1:5553"
		case "ip6tables":
			if r.LANIPv6 == "" {
				continue
			}
			match = "-m set --match-set UBIOS6ADDRv6_br0 dst"
			redirect = "-j REDIRECT --to-port 5553"
		}
		if err := r.run(
			iptables+" -t nat -N NEXTDNS",
			iptables+" -t nat -I PREROUTING 1 "+match+" -j NEXTDNS",
			iptables+" -t nat -A NEXTDNS -p udp -m udp --dport 53 "+redirect,
			iptables+" -t nat -A NEXTDNS -p tcp -m tcp --dport 53 "+redirect,
		); err != nil {
			return err
		}
	}
	ifaces, err := getInterfaceNames("br")
	if err != nil {
		return err
	}
	if len(ifaces) > 1 {
		for _, iface := range ifaces {
			if r.LANIPv6 == "" {
				continue
			}
			if iface == "br0" {
				continue
			}
			if err := r.run(
				"ip6tables -t nat -I PREROUTING 1 -m set --match-set UBIOS6ADDRv6_" + iface + " dst",
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Router) Restore() error {
	for _, iptables := range []string{"iptables", "ip6tables"} {
		var match string
		switch iptables {
		case "iptables":
			match = "-m addrtype --dst-type LOCAL"
		case "ip6tables":
			if r.LANIPv6 == "" {
				continue
			}
			match = "-m set --match-set UBIOS6ADDRv6_br0 dst"
		}
		if err := r.run(
			iptables+" -t nat -D PREROUTING "+match+" -j NEXTDNS",
			iptables+" -t nat -F NEXTDNS",
			iptables+" -t nat -X NEXTDNS",
		); err != nil {
			return err
		}
	}
	ifaces, err := getInterfaceNames("br")
	if err != nil {
		return err
	}
	if len(ifaces) > 1 {
		for _, iface := range ifaces {
			if r.LANIPv6 == "" {
				continue
			}
			if iface == "br0" {
				continue
			}
			if err := r.run(
				"ip6tables -t nat -D PREROUTING -m set --match-set UBIOS6ADDRv6_" + iface + " dst",
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Router) run(cmds ...string) error {
	var cmd *exec.Cmd
	if r.UsePodman {
		cmd = exec.Command("ssh", "-oStrictHostKeyChecking=no", "127.0.0.1", "sh", "-e", "-")
	} else {
		cmd = exec.Command("sh", "-e", "-")
	}
	cmd.Stdin = strings.NewReader(strings.Join(cmds, ";"))
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		err = fmt.Errorf("%v: %s", err, string(exitErr.Stderr))
	}
	return err
}
