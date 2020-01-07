// +build linux

package router

import (
	"github.com/nextdns/nextdns/router/ddwrt"
	"github.com/nextdns/nextdns/router/edgeos"
	"github.com/nextdns/nextdns/router/generic"
	"github.com/nextdns/nextdns/router/merlin"
	"github.com/nextdns/nextdns/router/openwrt"
	"github.com/nextdns/nextdns/router/synology"
)

func detectRouter() Router {
	if r, ok := openwrt.New(); ok {
		return r
	}
	if r, ok := merlin.New(); ok {
		return r
	}
	if r, ok := ddwrt.New(); ok {
		return r
	}
	if r, ok := edgeos.New(); ok {
		return r
	}
	if r, ok := synology.New(); ok {
		return r
	}
	return generic.New()
}
