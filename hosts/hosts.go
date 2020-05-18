package hosts

import "github.com/nextdns/nextdns/discovery"

var hosts = discovery.Hosts{}

func LookupAddr(addr string) []string {
	return hosts.LookupAddr(addr)
}

func LookupHost(name string) []string {
	return hosts.LookupHost(name)
}
