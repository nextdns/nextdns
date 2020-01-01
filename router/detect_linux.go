// +build linux

package router

import (
	"github.com/nextdns/nextdns/router/merlin"
	"github.com/nextdns/nextdns/router/openwrt"
)

func detectRouter() (Router, error) {
	if r, ok := openwrt.New(); ok {
		return r, nil
	}
	if r, ok := merlin.New(); ok {
		return r, nil
	}
	return nil, ErrRouterNotSupported
}
