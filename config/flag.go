package config

import (
	"bytes"
	"flag"
	"fmt"
	"net"
	"strings"
)

// Rule defines a configuration ID with some optional conditions.
type Rule struct {
	Config string
	Prefix *net.IPNet
	MAC    net.HardwareAddr
}

// ParseRule parses a configuration id with an optional condition.
func ParseRule(v string) (Rule, error) {
	idx := strings.IndexByte(v, '=')
	if idx == -1 {
		return Rule{Config: v}, nil
	}

	cond := strings.TrimSpace(v[:idx])
	conf := strings.TrimSpace(v[idx+1:])
	r := Rule{Config: conf}

	if _, ipnet, err := net.ParseCIDR(cond); err == nil {
		r.Prefix = ipnet
	} else if mac, err := net.ParseMAC(cond); err == nil {
		r.MAC = mac
	} else {
		return Rule{}, fmt.Errorf("%s: invalid condition format", cond)
	}
	return r, nil
}

// Match resturns true if the rule matches ip and mac.
func (r Rule) Match(ip net.IP, mac net.HardwareAddr) bool {
	fmt.Println(ip, mac, r)
	if r.Prefix != nil {
		if ip == nil {
			return false
		}
		if !r.Prefix.Contains(ip) {
			return false
		}
	}
	if r.MAC != nil {
		if mac == nil {
			return false
		}
		if bytes.Equal(r.MAC, mac) {
			return false
		}
	}
	return true
}

// Rules is a list of Rule.
type Rules []Rule

// Get returns the configuration matching the ip and mac conditions.
func (rs *Rules) Get(ip net.IP, mac net.HardwareAddr) string {
	for _, r := range *rs {
		if r.Match(ip, mac) {
			return r.Config
		}
	}
	return ""
}

// String is the method to format the flag's value
func (rs *Rules) String() string {
	return fmt.Sprint(*rs)
}

// Set is the method to set the flag value, part of the flag.Value interface.
func (rs *Rules) Set(value string) error {
	r, err := ParseRule(value)
	if err != nil {
		return err
	}
	*rs = append(*rs, r)
	return nil
}

// Flag defines a string flag defining configuration rule. The flag can be
// repeated.
func Flag(name, usage string) *Rules {
	rs := &Rules{}
	flag.Var(rs, name, usage)
	return rs
}
