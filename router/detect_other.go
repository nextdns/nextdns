// +build !linux

package router

import "github.com/nextdns/nextdns/router/generic"

func detectRouter() Router {
	return generic.New()
}
