// +build linux

package router

import "github.com/nextdns/nextdns/router/merlin"

func detectRouter() (Router, error) {
	if r, ok := merlin.New(); ok {
		return r, nil
	}
	return nil, ErrRouterNotSupported
}
