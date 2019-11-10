package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Endpoint represents a DoH server endpoint.
type Endpoint struct {
	// Hostname use to contact the DoH server. If Bootstrap is provided,
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
func New(hostname, path, bootstrap string) Endpoint {
	return Endpoint{
		Hostname:  hostname,
		Path:      path,
		Bootstrap: bootstrap,
	}
}

func (e Endpoint) String() string {
	if e.Bootstrap != "" {
		return fmt.Sprintf("https://%s[%s]%s", e.Hostname, e.Bootstrap, e.Path)
	} else {
		return fmt.Sprintf("https://%s%s", e.Hostname, e.Path)
	}
}

// Provider is a type responsible for producing a list of Endpoint.
type Provider interface {
	GetEndpoints(ctx context.Context) ([]Endpoint, error)
}

// StaticProvider wraps a Endpoint to adapt it to the Provider interface.
type StaticProvider Endpoint

// GetEndpoints implements the Provider interface.
func (p StaticProvider) GetEndpoints(ctx context.Context) ([]Endpoint, error) {
	return []Endpoint{Endpoint(p)}, nil
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
