package config

import (
	"bytes"
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
	if len(c.MAC) > 0 {
		if len(mac) == 0 {
			return false
		}
		if !bytes.Equal(c.MAC, mac) {
			return false
		}
	}
	return true
}

func (c config) isDefault() bool {
	return c.Prefix == nil && len(c.MAC) == 0
}

func (c config) String() string {
	if c.MAC != nil {
		return fmt.Sprintf("%s=%s", c.MAC, c.Config)
	}
	if c.Prefix != nil {
		return fmt.Sprintf("%s=%s", c.Prefix, c.Config)
	}
	return c.Config
}

// Configs is a list of Config with rules.
type Configs []config

// Get returns the configuration matching the ip and mac conditions.
func (cs *Configs) Get(ip net.IP, mac net.HardwareAddr) string {
	var def string
	for _, c := range *cs {
		if c.Match(ip, mac) {
			if c.isDefault() {
				def = c.Config
				continue
			}
			return c.Config
		}
	}
	return def
}

// String is the method to format the flag's value
func (cs *Configs) String() string {
	return fmt.Sprint(*cs)
}

func (cs *Configs) Strings() []string {
	if cs == nil {
		return nil
	}
	var s []string
	for _, c := range *cs {
		s = append(s, c.String())
	}
	return s
}

// Set is the method to set the flag value, part of the flag.Value interface.
func (cs *Configs) Set(value string) error {
	c, err := newConfig(value)
	if err != nil {
		return err
	}
	// Replace if c match the same criteria of an existing config
	for i, _c := range *cs {
		if (c.MAC != nil && _c.MAC != nil && bytes.Equal(c.MAC, _c.MAC)) ||
			(c.Prefix != nil && _c.Prefix != nil && c.Prefix.String() == _c.Prefix.String()) ||
			(c.MAC == nil && c.Prefix == nil && _c.MAC == nil && _c.Prefix == nil) {
			(*cs)[i] = c
			return nil
		}
	}
	*cs = append(*cs, c)
	return nil
}
