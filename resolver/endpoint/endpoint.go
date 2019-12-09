package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/nextdns/nextdns/host"
)

type Protocol int

func (p Protocol) String() string {
	switch p {
	case ProtocolDOH:
		return "doh"
	case ProtocolDNS:
		return "dns"
	default:
		return "unknown"
	}
}

const (
	ProtocolDOH Protocol = iota
	ProtocolDNS
)

// Endpoint represents a DNS server endpoint.
type Endpoint interface {
	fmt.Stringer

	// Protocol returns the protocol used by this endpoint to transport DNS.
	Protocol() Protocol

	// Equal returns true if e represent the same endpoint.
	Equal(e Endpoint) bool

	// Test valides the endpoint is up or return an error otherwise.
	Test(ctx context.Context, testDomain string) error
}

// New is a convenient method to build a Endpoint.
//
// Supported format for server are:
//
//   * DoH:   https://doh.server.com/path
//   * DoH:   https://doh.server.com/path#1.2.3.4 // with bootstrap
//   * DNS53: 1.2.3.4
//   * DNS53: 1.2.3.4:5353
func New(server string) (Endpoint, error) {
	if strings.HasPrefix(server, "https://") {
		u, err := url.Parse(server)
		if err != nil {
			return nil, err
		}
		e := &DOHEndpoint{
			Hostname:  u.Host,
			Path:      u.Path,
			Bootstrap: strings.Split(u.Fragment, ","),
		}
		return e, nil
	}

	host, port, err := net.SplitHostPort(server)
	if err != nil {
		host = server
		port = "53"
	}
	if ip := net.ParseIP(host); ip == nil {
		return nil, errors.New("not a valid IP address")
	}
	return &DNSEndpoint{
		Addr: net.JoinHostPort(host, port),
	}, nil
}

// MustNew is like New but panics on error.
func MustNew(server string) Endpoint {
	e, err := New(server)
	if err != nil {
		panic(err.Error())
	}
	return e
}

// Provider is a type responsible for producing a list of Endpoint.
type Provider interface {
	GetEndpoints(ctx context.Context) ([]Endpoint, error)
}

// StaticProvider wraps a Endpoint slice to adapt it to the Provider interface.
type StaticProvider []Endpoint

// GetEndpoints implements the Provider interface.
func (p StaticProvider) GetEndpoints(ctx context.Context) ([]Endpoint, error) {
	return p, nil
}

// SourceURLProvider loads a list of endpoints from a remote URL.
type SourceURLProvider struct {
	// SourceURL is a URL pointing to a JSON resource listing one or more
	// Endpoints.
	SourceURL string

	// Client is the http.Client to use to fetch SourceURL. If not defined,
	// http.DefaultClient is used.
	Client *http.Client

	mu            sync.Mutex
	prevEndpoints []Endpoint
}

func (p *SourceURLProvider) String() string {
	return p.SourceURL
}

// GetEndpoints implements the Provider interface.
func (p *SourceURLProvider) GetEndpoints(ctx context.Context) ([]Endpoint, error) {
	c := p.Client
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequest("GET", p.SourceURL, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var dohEndpoints []*DOHEndpoint
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&dohEndpoints)
	if err != nil {
		return nil, err
	}
	endpoints := make([]Endpoint, len(dohEndpoints))
	for i := range dohEndpoints {
		endpoints[i] = dohEndpoints[i]
	}
	// Reuse previous endpoints when identical so we keep our conn pools warm.
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, e := range endpoints {
		for _, pe := range p.prevEndpoints {
			if e.Equal(pe) {
				endpoints[i] = pe
			}
		}
	}
	p.prevEndpoints = endpoints
	return endpoints, nil
}

type SystemDNSProvider struct {
}

func (p SystemDNSProvider) String() string {
	return "SystemDNSProvider"
}

func (p SystemDNSProvider) GetEndpoints(ctx context.Context) ([]Endpoint, error) {
	ips, err := host.DNS()
	if err != nil {
		return nil, err
	}
	endpoints := make([]Endpoint, 0, len(ips))
	for _, ip := range ips {
		endpoints = append(endpoints, &DNSEndpoint{
			Addr: net.JoinHostPort(ip, "53"),
		})
	}
	fmt.Println("captive", endpoints)
	return endpoints, nil
}
