package endpoint

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type ClientInfo struct {
	ID    string
	IP    string
	Model string
	Name  string
}

// Endpoint represents a DoH  server endpoint.
type DOHEndpoint struct {
	// Hostname use to contact the DoH server. If Bootstrap is provided,
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

func (e *DOHEndpoint) Protocol() Protocol {
	return ProtocolDOH
}

func (e *DOHEndpoint) Equal(e2 Endpoint) bool {
	if e2, ok := e2.(*DOHEndpoint); ok {
		return e.Hostname == e2.Hostname && e.Path == e2.Path
	}
	return false
}

func (e *DOHEndpoint) String() string {
	if len(e.Bootstrap) != 0 {
		return fmt.Sprintf("https://%s%s#%s", e.Hostname, e.Path, strings.Join(e.Bootstrap, ","))
	}
	return fmt.Sprintf("https://%s%s", e.Hostname, e.Path)
}

func (e *DOHEndpoint) Test(ctx context.Context, testDomain string) (err error) {
	req, _ := http.NewRequest("GET", "https://nowhere?name="+testDomain, nil)
	req = req.WithContext(ctx)
	res, err := e.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("roundtrip: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status: %d", res.StatusCode)
	}
	return nil
}

func (e *DOHEndpoint) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	e.once.Do(func() {
		if e.transport == nil {
			e.transport = newTransport(e)
		}
	})
	return e.transport.RoundTrip(req)
}
