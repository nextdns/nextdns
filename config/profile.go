package config

import (
	"bytes"
	"fmt"
	"net"
	"strings"
)

// profile defines a profile ID with some optional conditions.
type profile struct {
	ID     string
	Prefix *net.IPNet
	MAC    net.HardwareAddr
}

// newConfig parses a configuration id with an optional condition.
func newConfig(v string) (profile, error) {
	idx := strings.IndexByte(v, '=')
	if idx == -1 {
		return profile{ID: v}, nil
	}

	cond := strings.TrimSpace(v[:idx])
	conf := strings.TrimSpace(v[idx+1:])
	c := profile{ID: conf}

	if _, ipnet, err := net.ParseCIDR(cond); err == nil {
		c.Prefix = ipnet
	} else if mac, err := net.ParseMAC(cond); err == nil {
		c.MAC = mac
	} else {
		return profile{}, fmt.Errorf("%s: invalid condition format", cond)
	}
	return c, nil
}

// Match resturns true if the rule matches ip and mac.
func (p profile) Match(ip net.IP, mac net.HardwareAddr) bool {
	if p.Prefix != nil {
		if ip == nil {
			return false
		}
		if !p.Prefix.Contains(ip) {
			return false
		}
	}
	if len(p.MAC) > 0 {
		if len(mac) == 0 {
			return false
		}
		if !bytes.Equal(p.MAC, mac) {
			return false
		}
	}
	return true
}

func (p profile) isDefault() bool {
	return p.Prefix == nil && len(p.MAC) == 0
}

func (p profile) String() string {
	if p.MAC != nil {
		return fmt.Sprintf("%s=%s", p.MAC, p.ID)
	}
	if p.Prefix != nil {
		return fmt.Sprintf("%s=%s", p.Prefix, p.ID)
	}
	return p.ID
}

// Profiles is a list of profile with rules.
type Profiles []profile

// Get returns the configuration matching the ip and mac conditions.
func (ps *Profiles) Get(ip net.IP, mac net.HardwareAddr) string {
	var def string
	for _, p := range *ps {
		if p.Match(ip, mac) {
			if p.isDefault() {
				def = p.ID
				continue
			}
			return p.ID
		}
	}
	return def
}

// String is the method to format the flag's value
func (ps *Profiles) String() string {
	return fmt.Sprint(*ps)
}

func (ps *Profiles) Strings() []string {
	if ps == nil {
		return nil
	}
	var s []string
	for _, p := range *ps {
		s = append(s, p.String())
	}
	return s
}

// Set is the method to set the flag value, part of the flag.Value interface.
func (ps *Profiles) Set(value string) error {
	p, err := newConfig(value)
	if err != nil {
		return err
	}
	// Replace if c match the same criteria of an existing config
	for i, _p := range *ps {
		if (p.MAC != nil && _p.MAC != nil && bytes.Equal(p.MAC, _p.MAC)) ||
			(p.Prefix != nil && _p.Prefix != nil && p.Prefix.String() == _p.Prefix.String()) ||
			(p.MAC == nil && p.Prefix == nil && _p.MAC == nil && _p.Prefix == nil) {
			(*ps)[i] = p
			return nil
		}
	}
	*ps = append(*ps, p)
	return nil
}
