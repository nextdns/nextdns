package generic

import (
	"github.com/nextdns/nextdns/config"
)

type Router struct {
}

func New() *Router {
	return &Router{}
}

func (r *Router) Configure(c *config.Config) {
	c.Listen = ":53"
}

func (r *Router) Setup() error {
	return nil
}

func (r *Router) Restore() error {
	return nil
}
