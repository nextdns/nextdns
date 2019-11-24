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

	// Bootstrap is the IPs to use to contact the DoH server. When provided, no
	// DNS request is necessary to contact the DoH server. The fastest IP is
	// used.
	Bootstrap []string `json:"ips"`

	once      sync.Once
	transport http.RoundTripper
}

// New is a convenient method to build a Endpoint.
//
// Supported format for server are:
//
//   * DoH:   https://doh.server.com/path
//   * DoH:   https://doh.server.com/path#1.2.3.4 // with bootstrap
//   * DNS53: 1.2.3.4
func New(server string) (*Endpoint, error) {
	if strings.HasPrefix(server, "https://") {
		u, err := url.Parse(server)
		if err != nil {
			return nil, err
		}
		e := &Endpoint{
			Protocol:  ProtocolDOH,
			Hostname:  u.Host,
			Path:      u.Path,
			Bootstrap: strings.Split(u.Fragment, ","),
		}
		return e, nil
	}

	if ip := net.ParseIP(server); ip == nil {
		return nil, errors.New("not a valid IP address")
	}
	return &Endpoint{
		Protocol: ProtocolDNS,
		Hostname: net.JoinHostPort(server, "53"),
	}, nil
}

// MustNew is like New but panics on error.
func MustNew(server string) *Endpoint {
	e, err := New(server)
	if err != nil {
		panic(err.Error())
	}
	return e
}

func (e *Endpoint) Equal(e2 *Endpoint) bool {
	return e.Protocol == e2.Protocol &&
		e.Hostname == e2.Hostname &&
		e.Path == e2.Path
}

func (e *Endpoint) String() string {
	if e.Protocol == ProtocolDNS {
		return e.Hostname
	}
	if len(e.Bootstrap) != 0 {
		return fmt.Sprintf("https://%s%s#%s", e.Hostname, e.Path, strings.Join(e.Bootstrap, ","))
	}
	return fmt.Sprintf("https://%s%s", e.Hostname, e.Path)
}

func (e *Endpoint) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	e.once.Do(func() {
		if e.transport == nil {
			e.transport = newTransport(e)
		}
	})
	return e.transport.RoundTrip(req)
}

func (e *Endpoint) Test(ctx context.Context, testDomain string) (err error) {
	switch e.Protocol {
	case ProtocolDOH:
		err = testDOH(ctx, testDomain, e)
	case ProtocolDNS:
		err = testDNS(ctx, testDomain, e.Hostname)
	default:
		panic("unsupported protocol")
	}
	if err != nil {
		err = fmt.Errorf("%s test: %v", e.Protocol, err)
	}
	return err
}

// Provider is a type responsible for producing a list of Endpoint.
type Provider interface {
	GetEndpoints(ctx context.Context) ([]*Endpoint, error)
}

// StaticProvider wraps a Endpoint slice to adapt it to the Provider interface.
type StaticProvider []*Endpoint

// GetEndpoints implements the Provider interface.
func (p StaticProvider) GetEndpoints(ctx context.Context) ([]*Endpoint, error) {
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
	prevEndpoints []*Endpoint
}

// GetEndpoints implements the Provider interface.
func (p *SourceURLProvider) GetEndpoints(ctx context.Context) ([]*Endpoint, error) {
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
	var endpoints []*Endpoint
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&endpoints)
	if err != nil {
		return nil, err
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
