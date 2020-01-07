package router

import (
	"errors"

	"github.com/nextdns/nextdns/config"
)

type Router interface {
	// Configure reads and changes c to match router's needs.
	// Ran before listen.
	Configure(c *config.Config) error

	// Setup configures the router to work with NextDNS.
	// Ran after listen.
	Setup() error

	// Restore restores the router configuration.
	// Ran after stop listening.
	Restore() error
}

var ErrRouterNotSupported = errors.New("router not supported")

func New() Router {
	return detectRouter()
}
