package config

import (
	"bytes"
	"fmt"
	"net"
	"strings"
)

// profile defines a profile ID with some optional conditions.
type profile struct {
	ID      string
	Prefix  *net.IPNet
	MAC     net.HardwareAddr
	DestIPs []net.IP
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
	} else if iface, _ := net.InterfaceByName(cond); iface != nil {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				c.DestIPs = append(c.DestIPs, ipnet.IP)
			}
		}
	} else {
		if err != nil {
			return profile{}, fmt.Errorf("%s: invalid condition format or non-existant interface name", cond)
		}
	}
	return c, nil
}

// Match returns true if the rule matches ip or interface and mac.
func (p profile) Match(sourceIP, destIP net.IP, mac net.HardwareAddr) bool {
	if p.Prefix != nil {
		if sourceIP == nil {
			return false
		}
		if !p.Prefix.Contains(sourceIP) {
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
	if len(p.DestIPs) > 0 {
		if destIP == nil {
			return false
		}
		for i := range p.DestIPs {
			if p.DestIPs[i].Equal(destIP) {
				return true
			}
		}
		return false
	}
	return true
}

func (p profile) isDefault() bool {
	return p.Prefix == nil && len(p.MAC) == 0 && len(p.DestIPs) == 0
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
func (ps *Profiles) Get(sourceIP, destIP net.IP, mac net.HardwareAddr) string {
	var def string
	for _, p := range *ps {
		if p.Match(sourceIP, destIP, mac) {
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
			(p.DestIPs != nil && _p.DestIPs != nil && ipListEqual(p.DestIPs, _p.DestIPs)) ||
			(p.Prefix != nil && _p.Prefix != nil && p.Prefix.String() == _p.Prefix.String()) ||
			(p.MAC == nil && p.Prefix == nil && p.DestIPs == nil && _p.MAC == nil && _p.Prefix == nil && _p.DestIPs == nil) {
			(*ps)[i] = p
			return nil
		}
	}
	*ps = append(*ps, p)
	return nil
}

func ipListEqual(a, b []net.IP) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}
