package main

import (
	"fmt"
	"net"

	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/host"
)

func activation(args []string) error {
	cmd := args[0]
	var c config.Config
	c.Parse("nextdns "+cmd, nil, true)
	defer c.Save()
	switch cmd {
	case "activate":
		c.AutoActivate = true
		return activate(c.Listen)
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
	addrs, err := net.LookupHost(host)
	if err != nil {
		return "", fmt.Errorf("activate: %s: %v", listen, err)
	}
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
