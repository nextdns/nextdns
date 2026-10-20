package endpoint

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestManager_ProbesRetainedEndpoint(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "default tester"
		if custom {
			name = "custom tester"
		}
		t.Run(name, func(t *testing.T) {
			for _, failed := range []bool{false, true} {
				name := "healthy"
				if failed {
					name = "failed"
				}
				t.Run(name, func(t *testing.T) {
					activeRT := &errTransport{}
					active := &DOHEndpoint{Hostname: "primary", transport: activeRT}
					freshProbes := 0
					fresh := &DOHEndpoint{Hostname: "primary", transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
						freshProbes++
						return (&errTransport{}).RoundTrip(req)
					})}
					fallback := &DOHEndpoint{Hostname: "fallback", transport: &errTransport{}}
					var reported Endpoint
					changes := 0
					m := &Manager{
						Providers: []Provider{StaticProvider{active}},
						OnError:   func(e Endpoint, _ error) { reported = e },
						OnChange:  func(Endpoint) { changes++ },
					}
					if custom {
						m.EndpointTester = func(e Endpoint) Tester {
							if e == fresh {
								t.Error("custom tester received the fresh endpoint")
							}
							return endpointTester(e)
						}
					}
					if err := m.Test(context.Background()); err != nil {
						t.Fatal(err)
					}
					if failed {
						activeRT.errs = []error{errors.New("active transport failed")}
					}
					m.Providers = []Provider{StaticProvider{fresh, fallback}}
					if err := m.Test(context.Background()); err != nil {
						t.Fatal(err)
					}
					if freshProbes != 0 {
						t.Errorf("fresh transport was probed %d times instead of the retained transport", freshProbes)
					}
					want, wantChanges := active, 1
					if failed {
						want, wantChanges = fallback, 2
						if reported != active {
							t.Errorf("reported endpoint = %p, want retained endpoint %p", reported, active)
						}
					} else if reported != nil {
						t.Errorf("unexpected failed endpoint: %v", reported)
					}
					if got := m.activeEndpoint.Load().Endpoint; got != want {
						t.Errorf("active endpoint = %p, want %p", got, want)
					}
					if changes != wantChanges {
						t.Errorf("OnChange calls = %d, want %d", changes, wantChanges)
					}
				})
			}
		})
	}
}

type closeTrackingTransport struct {
	http.RoundTripper
	closes int
}

func (t *closeTrackingTransport) CloseIdleConnections() { t.closes++ }

func TestManager_ClosesFailedProbeTransport(t *testing.T) {
	failedRT := &closeTrackingTransport{RoundTripper: &errTransport{errs: []error{errors.New("probe failed")}}}
	healthyRT := &closeTrackingTransport{RoundTripper: &errTransport{}}
	failed := &DOHEndpoint{Hostname: "failed", transport: failedRT}
	healthy := &DOHEndpoint{Hostname: "healthy", transport: healthyRT}
	m := &Manager{Providers: []Provider{StaticProvider{failed, healthy}}}
	if err := m.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	if failedRT.closes != 1 {
		t.Errorf("failed probe idle cleanup calls = %d, want 1", failedRT.closes)
	}
	if healthyRT.closes != 0 {
		t.Errorf("healthy transport was closed %d times", healthyRT.closes)
	}
	if m.activeEndpoint.Load().Endpoint != healthy {
		t.Error("healthy endpoint was not selected")
	}
}
