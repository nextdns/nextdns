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
)

type Protocol int

const (
	ProtocolDOH Protocol = iota
	ProtocolDNS
)

// Endpoint represents a DoH or DNS53 server endpoint.
type Endpoint struct {
	// Protocol defines the protocol to use with this endpoint. The default if
	// DOH. When DNS is specified, Path and Bootstrap are ignored.
	Protocol Protocol

	// Hostname use to contact the DoH or DNS server. If Bootstrap is provided,
	// Hostname is only used for TLS verification.
	Hostname string

	// Path to use with DoH HTTP requests. If empty, the path received in the
	// request by Transport is left untouched.
	Path string

	// Bootstrap is the IP to use to contact the DoH server. When provided, no
	// DNS request is necessary to contact the DoH server.
	Bootstrap string `json:"ip"`
}

// New is a convenient method to build a Endpoint.
//
// Supported format for server are:
//
//   * DoH:   https://doh.server.com/path
//   * DoH:   https://doh.server.com/path#1.2.3.4 // with bootstrap
//   * DNS53: 1.2.3.4
func New(server string) (Endpoint, error) {
	if strings.HasPrefix(server, "https://") {
		u, err := url.Parse(server)
		if err != nil {
			return Endpoint{}, err
		}
		e := Endpoint{
			Protocol:  ProtocolDOH,
			Hostname:  u.Host,
			Path:      u.Path,
			Bootstrap: u.Fragment,
		}
		return e, nil
	}

	if ip := net.ParseIP(server); ip == nil {
		return Endpoint{}, errors.New("not a valid IP address")
	}
	return Endpoint{
		Protocol: ProtocolDNS,
		Hostname: net.JoinHostPort(server, "53"),
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

func (e Endpoint) String() string {
	if e.Protocol == ProtocolDNS {
		return e.Hostname
	}
	if e.Bootstrap != "" {
		return fmt.Sprintf("https://%s@%s%s", e.Hostname, e.Bootstrap, e.Path)
	}
	return fmt.Sprintf("https://%s%s", e.Hostname, e.Path)
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
}

// GetEndpoints implements the Provider interface.
func (p SourceURLProvider) GetEndpoints(ctx context.Context) ([]Endpoint, error) {
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
	var endpoints []Endpoint
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&endpoints)
	return endpoints, err
}
