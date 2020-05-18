package discovery

import (
	"strings"
)

type Resolver []Source

type Source interface {
	Name() string
	Visit(func(name string, addrs []string))
	LookupAddr(addr string) []string
	LookupHost(addr string) []string
}

type sourceMAC interface {
	LookupMAC(mac string) []string
}

func (r Resolver) Visit(f func(source, name string, addr []string)) {
	for _, s := range r {
		sn := s.Name()
		s.Visit(func(name string, addrs []string) {
			f(sn, name, addrs)
		})
	}
}

func (r Resolver) LookupAddr(addr string) []string {
	addr = strings.ToLower(addr)
	for _, s := range r {
		if names := s.LookupAddr(addr); len(names) > 0 {
			return names
		}
	}
	return nil
}

func (r Resolver) LookupHost(name string) []string {
	name = strings.ToLower(name)
	for _, s := range r {
		if addrs := s.LookupHost(name); len(addrs) > 0 {
			return addrs
		}
	}
	return nil
}

func (r Resolver) LookupMAC(mac string) []string {
	mac = strings.ToLower(mac)
	for _, s := range r {
		if s, ok := s.(sourceMAC); ok {
			if names := s.LookupMAC(mac); len(names) > 0 {
				return names
			}
		}
	}
	return nil
}
