package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextdns/nextdns/resolver"
)

// Resolver defines a forwarder server with some optional conditions.
type Resolver struct {
	resolver.Resolver
	addr   string
	Domain string
}

// newResolver parses a server definition with an optional condition.
func newResolver(v string) (Resolver, error) {
	idx := strings.IndexByte(v, '=')
	var r Resolver
	r.addr = v
	if idx != -1 {
		r.addr = strings.TrimSpace(v[idx+1:])
		r.Domain = fqdn(strings.TrimSpace(v[:idx]))
	}
	var err error
	r.Resolver, err = resolver.New(r.addr)
	return r, err
}

// Match resturns true if the rule matches domain.
func (r Resolver) Match(domain string) bool {
	if r.Domain != "" {
		if domain != r.Domain && !isSubDomain(domain, r.Domain) {
			return false
		}
	}
	return true
}

func (r Resolver) String() string {
	if r.Domain != "" {
		return fmt.Sprintf("%s=%s", r.Domain, r.addr)
	}
	return r.addr
}

func fqdn(s string) string {
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func isSubDomain(sub, domain string) bool {
	return strings.HasSuffix(sub, "."+domain)
}

// Forwarders is a list of Resolver with rules.
type Forwarders []Resolver

// Get returns the server matching the domain conditions.
func (f *Forwarders) Get(domain string) resolver.Resolver {
	for _, s := range *f {
		if s.Match(domain) {
			return s.Resolver
		}
	}
	return nil
}

// String is the method to format the flag's value
func (f *Forwarders) String() string {
	return fmt.Sprint(*f)
}

func (f *Forwarders) Strings() []string {
	if f == nil {
		return nil
	}
	var s []string
	for _, r := range *f {
		s = append(s, r.String())
	}
	return s
}

// Set is the method to set the flag value, part of the flag.Value interface.
func (f *Forwarders) Set(value string) error {
	r, err := newResolver(value)
	if err != nil {
		return err
	}
	for i, _r := range *f {
		if r.Domain == _r.Domain {
			(*f)[i] = r
			return nil
		}
	}
	*f = append(*f, r)
	return nil
}

// Resolve implements proxy.Resolver interface.
func (f *Forwarders) Resolve(ctx context.Context, q resolver.Query, buf []byte) (int, resolver.ResolveInfo, error) {
	r := f.Get(q.Name)
	if r == nil {
		return -1, resolver.ResolveInfo{}, fmt.Errorf("%s: no forwarder defined", q.Name)
	}
	return r.Resolve(ctx, q, buf)
}
