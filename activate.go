package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/hosts"
)

func activation(args []string) error {
	cmd := args[0]
	var c config.Config
	c.Parse("nextdns "+cmd, nil, true)
	defer func() { _ = c.Save() }()
	switch cmd {
	case "activate":
		c.AutoActivate = true
		return activate(c)
	case "deactivate":
		c.AutoActivate = false
		return deactivate()
	default:
		return fmt.Errorf("%s: unknown command", cmd)
	}
}

func listenAddr(listen string) (string, uint16, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "127.0.0.1", 53, nil
	}
	var p uint16
	switch port {
	case "53", "domain":
		p = 53
	default:
		port, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return "", 0, fmt.Errorf("activate: %s: invalid port: %v", listen, err)
		}
		p = uint16(port)
	}
	switch host {
	case "", "0.0.0.0":
		return "127.0.0.1", p, nil
	case "::":
		return "::1", p, nil
	}
	if net.ParseIP(host) != nil {
		return host, p, nil
	}
	addrs := hosts.LookupHost(host)
	if len(addrs) == 0 {
		return "", 0, fmt.Errorf("activate: %s: no address found", listen)
	}
	return addrs[0], p, nil
}

func activate(c config.Config) error {
	if len(c.Listens) == 0 {
		return errors.New("missing listen setting")
	}
	listen := c.Listens[0]
	if c.SetupRouter {
		// Setup router might make nextdns listen on a custom port so it can
		// be chained behind dnsmasq for instance. To make the router use
		// nextdns, we want it to go thru the whole chain so it benefits
		// from dnsmasq cache.
		listen = "127.0.0.1:53"
	}
	listenIP, listenPort, err := listenAddr(listen)
	if err != nil {
		return err
	}
	return host.SetDNS(listenIP, listenPort)
}

func deactivate() error {
	return host.ResetDNS()
}
