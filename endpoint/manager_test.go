package endpoint

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

type errTransport struct {
	errs []error
}

func (t *errTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var err error
	if len(t.errs) > 0 {
		err = t.errs[0]
		if len(t.errs) > 1 { // keep last err
			t.errs = t.errs[1:]
		}
	}
	return &http.Response{StatusCode: http.StatusOK}, err
}

type testManager struct {
	Manager

	mu         sync.Mutex
	transports map[string]*errTransport
	elected    string
	electedRT  http.RoundTripper
	errs       []string
}

func (m *testManager) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.electedRT.RoundTrip(req)
}

func (m *testManager) wantElected(t *testing.T, wantElected string) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if got, want := m.elected, wantElected; got != want {
		t.Errorf("Elected %v, want %v", got, want)
	}
}

func (m *testManager) wantErrors(t *testing.T, wantErrors []string) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if got, want := m.errs, wantErrors; !reflect.DeepEqual(got, want) {
		t.Errorf("Test() errs %v, want %v", got, want)
	}
}

func newTestManager() *testManager {
	m := &testManager{
		transports: map[string]*errTransport{
			"a": &errTransport{},
			"b": &errTransport{},
		},
		errs: []string{},
	}
	m.Manager = Manager{
		Providers: []Provider{
			StaticProvider(Endpoint{Hostname: "a"}),
			StaticProvider(Endpoint{Hostname: "b"}),
		},
		ErrorThreshold:  5,
		MinTestInterval: 100 * time.Millisecond,
		OnChange: func(e Endpoint, rt http.RoundTripper) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.elected = e.Hostname
			m.electedRT = rt
		},
		OnError: func(e Endpoint, err error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.errs = append(m.errs, err.Error())
		},
		testNewTransport: func(e Endpoint) *managerTransport {
			return &managerTransport{
				RoundTripper: m.transports[e.Hostname],
				Endpoint:     e,
				Manager:      m.Manager,
				lastTest:     time.Now(),
			}
		},
	}
	return m
}

func TestManager_SteadyState(t *testing.T) {
	m := newTestManager()

	_ = m.Test(context.Background())
	m.wantElected(t, "a")
	m.wantErrors(t, []string{})
}

func TestManager_FirstFail(t *testing.T) {
	m := newTestManager()

	m.transports["a"].errs = []error{errors.New("a failed")}

	_ = m.Test(context.Background())
	m.wantElected(t, "b")
	m.wantErrors(t, []string{"a failed"})
}

func TestManager_FirstAllThenRecover(t *testing.T) {
	m := newTestManager()

	m.transports["a"].errs = []error{errors.New("a failed"), nil} // fails once then recover
	m.transports["b"].errs = []error{errors.New("b failed")}

	_ = m.Test(context.Background())
	m.wantElected(t, "a")
	m.wantErrors(t, []string{"a failed", "b failed"})
}

func TestManager_AutoRecover(t *testing.T) {
	// Fail none at init, then make enough consecutive errors to trigger a switch to second endpoint
	m := newTestManager()

	m.transports["a"].errs = []error{nil, errors.New("a failed")} // succeed first req, then error
	m.transports["b"].errs = nil

	_ = m.Test(context.Background())
	m.wantElected(t, "a")
	for i, wantElected := range []string{"a", "a", "a", "a", "a", "b", "b"} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			_, _ = m.RoundTrip(&http.Request{})
			m.wantElected(t, wantElected)
			runtime.Gosched() // recovery happens in a goroutine
		})
	}
}

func TestManager_OpportunisticTest(t *testing.T) {
	// Start with first endpoint failed, then recover it to ensure the client eventually goes back to it.
	m := newTestManager()

	m.transports["a"].errs = []error{errors.New("a failed"), nil} // fails once then recover
	m.transports["b"].errs = nil

	_ = m.Test(context.Background())
	m.wantElected(t, "b")
	for i, wantElected := range []string{"b", "b", "a"} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			time.Sleep(60 * time.Millisecond)
			_, _ = m.RoundTrip(&http.Request{})
			m.wantElected(t, wantElected)
		})
	}
}
