package main

import (
	"fmt"

	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/host"
)

func activation(cmd string) error {
	var c config.Config
	c.Parse(nil)
	defer c.Save()
	switch cmd {
	case "activate":
		c.AutoActivate = true
		return activate()
	case "deactivate":
		c.AutoActivate = false
		return deactivate()
	default:
		return fmt.Errorf("%s: unknown command", cmd)
	}
}

func activate() error {
	return host.SetDNS("127.0.0.1")
}

func deactivate() error {
	return host.ResetDNS()
}
