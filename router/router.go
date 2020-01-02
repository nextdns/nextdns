package router

import (
	"errors"

	"github.com/nextdns/nextdns/config"
)

type Router interface {
	// Configure reads and changes c to match router's needs.
	Configure(c *config.Config)

	// Setup configures the router to work with NextDNS.
	Setup() error

	// Restore restores the router configuration.
	Restore() error
}

var ErrRouterNotSupported = errors.New("router not supported")

func New() Router {
	return detectRouter()
}
