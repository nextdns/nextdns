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
	now        time.Time
	errs       []string
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

func (m *testManager) addTime(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
}

func newTestManager(t *testing.T) *testManager {
	m := &testManager{
		transports: map[string]*errTransport{
			"a": &errTransport{},
			"b": &errTransport{},
		},
		now:  time.Now(),
		errs: []string{},
	}
	m.Manager = Manager{
		Providers: []Provider{
			StaticProvider([]Endpoint{
				Endpoint{Hostname: "a"},
				Endpoint{Hostname: "b"},
			}),
		},
		OnChange: func(e Endpoint) {
			m.mu.Lock()
			defer m.mu.Unlock()
			t.Logf("endpoing changed to %v", e)
			m.elected = e.Hostname
		},
		OnError: func(e Endpoint, err error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			t.Logf("endpoing err %v: %v", e, err)
			m.errs = append(m.errs, err.Error())
		},
		testNewTransport: func(e Endpoint) http.RoundTripper {
			return m.transports[e.Hostname]
		},
		testNow: func() time.Time {
			m.mu.Lock()
			defer m.mu.Unlock()
			return m.now
		},
	}
	return m
}

func TestManager_SteadyState(t *testing.T) {
	m := newTestManager(t)

	_ = m.Test(context.Background())
	m.wantElected(t, "a")
	m.wantErrors(t, []string{})
}

func TestManager_FirstFail(t *testing.T) {
	m := newTestManager(t)

	m.transports["a"].errs = []error{errors.New("a failed")}

	_ = m.Test(context.Background())
	m.wantElected(t, "b")
	m.wantErrors(t, []string{"a failed"})
}

func TestManager_FirstAllThenRecover(t *testing.T) {
	m := newTestManager(t)

	m.transports["a"].errs = []error{errors.New("a failed"), nil} // fails once then recover
	m.transports["b"].errs = []error{errors.New("b failed")}

	_ = m.Test(context.Background())
	m.wantElected(t, "a")
	m.wantErrors(t, []string{"a failed", "b failed"})
}

func TestManager_AutoRecover(t *testing.T) {
	// Fail none at init, then make enough consecutive errors to trigger a switch to second endpoint
	m := newTestManager(t)
	m.ErrorThreshold = 5

	m.transports["a"].errs = []error{nil, errors.New("a failed")} // succeed first req, then error
	m.transports["b"].errs = nil

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
	m := newTestManager(t)
	m.MinTestInterval = 100 * time.Millisecond

	m.transports["a"].errs = []error{errors.New("a failed"), nil} // fails once then recover
	m.transports["b"].errs = nil

	for i, wantElected := range []string{"b", "b", "b", "a"} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			_, _ = m.RoundTrip(&http.Request{})
			m.wantElected(t, wantElected)
			m.addTime(60 * time.Millisecond)
		})
	}
}
