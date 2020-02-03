package discovery

import (
	"github.com/nextdns/nextdns/hosts"
)

type Hosts struct{}

func (r Hosts) Lookup(addr string) (string, bool) {
	name := hosts.LookupAddr(addr)
	if len(name) == 0 {
		return "", false
	}
	return normalizeName(name[0]), true
}
