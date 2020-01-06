package main

import (
	"fmt"
	"net"

	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/hosts"
)

func activation(args []string) error {
	cmd := args[0]
	var c config.Config
	c.Parse("nextdns "+cmd, nil, true)
	defer c.Save()
	switch cmd {
	case "activate":
		c.AutoActivate = true
		listen := c.Listen
		if c.SetupRouter {
			// Setup router might make nextdns listen on a custom port so it can
			// be chained behind dnsmasq for instance. To make the router use
			// nextdns, we want it to go thru the whole chain so it benefits
			// from dnsmasq cache.
			listen = "127.0.0.1:53"
		}
		return activate(listen)
	case "deactivate":
		c.AutoActivate = false
		return deactivate()
	default:
		return fmt.Errorf("%s: unknown command", cmd)
	}
}

func listenIP(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "127.0.0.1", nil
	}
	switch port {
	case "53", "domain":
		// Can only activate on default port
	default:
		return "", fmt.Errorf("activate: %s: non 53 port not supported", listen)
	}
	switch host {
	case "", "0.0.0.0":
		return "127.0.0.1", nil
	case "::":
		return "::1", nil
	}
	addrs := hosts.LookupHost(host)
	if len(addrs) == 0 {
		return "", fmt.Errorf("activate: %s: no address found", listen)
	}
	return addrs[0], nil
}

func activate(listen string) error {
	listenIP, err := listenIP(listen)
	if err != nil {
		return err
	}
	return host.SetDNS(listenIP)
}

func deactivate() error {
	return host.ResetDNS()
}
