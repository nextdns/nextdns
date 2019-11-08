package config

import (
	"bytes"
	"flag"
	"fmt"
	"net"
	"strings"
)

// config defines a configuration ID with some optional conditions.
type config struct {
	Config string
	Prefix *net.IPNet
	MAC    net.HardwareAddr
}

// newConfig parses a configuration id with an optional condition.
func newConfig(v string) (config, error) {
	idx := strings.IndexByte(v, '=')
	if idx == -1 {
		return config{Config: v}, nil
	}

	cond := strings.TrimSpace(v[:idx])
	conf := strings.TrimSpace(v[idx+1:])
	c := config{Config: conf}

	if _, ipnet, err := net.ParseCIDR(cond); err == nil {
		c.Prefix = ipnet
	} else if mac, err := net.ParseMAC(cond); err == nil {
		c.MAC = mac
	} else {
		return config{}, fmt.Errorf("%s: invalid condition format", cond)
	}
	return c, nil
}

// Match resturns true if the rule matches ip and mac.
func (c config) Match(ip net.IP, mac net.HardwareAddr) bool {
	if c.Prefix != nil {
		if ip == nil {
			return false
		}
		if !c.Prefix.Contains(ip) {
			return false
		}
	}
	if c.MAC != nil {
		if mac == nil {
			return false
		}
		if bytes.Equal(c.MAC, mac) {
			return false
		}
	}
	return true
}

// Configs is a list of Config with rules.
type Configs []config

// Get returns the configuration matching the ip and mac conditions.
func (cs *Configs) Get(ip net.IP, mac net.HardwareAddr) string {
	for _, c := range *cs {
		if c.Match(ip, mac) {
			return c.Config
		}
	}
	return ""
}

// String is the method to format the flag's value
func (cs *Configs) String() string {
	return fmt.Sprint(*cs)
}

// Set is the method to set the flag value, part of the flag.Value interface.
func (cs *Configs) Set(value string) error {
	c, err := newConfig(value)
	if err != nil {
		return err
	}
	*cs = append(*cs, c)
	return nil
}

// Config defines a string flag defining configuration rule. The flag can be
// repeated.
func Config(name, usage string) *Configs {
	cs := &Configs{}
	flag.Var(cs, name, usage)
	return cs
}
