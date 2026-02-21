package endpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

type errProvider struct {
	err error
}

func (e *errProvider) String() string {
	return "errProvider"
}

func (e *errProvider) GetEndpoints(ctx context.Context) ([]Endpoint, error) {
	return nil, e.err
}

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
	if err != nil {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, err
	}
	var reqBody []byte
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
	}
	respBody := append([]byte(nil), reqBody...)
	if len(respBody) >= 3 {
		respBody[2] |= 0x80 // set QR bit.
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(respBody)),
	}, nil
}

type testManager struct {
	Manager

	mu          sync.Mutex
	transports  map[string]*errTransport
	elected     string
	now         time.Time
	errs        []string
	perrs       []string
	errProvider *errProvider
}

func (m *testManager) do() {
	_ = m.Do(context.Background(), func(e Endpoint) error {
		_, err := e.(*DOHEndpoint).RoundTrip(&http.Request{})
		return err
	})
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

func (m *testManager) wantProviderErrors(t *testing.T, wantErrors []string) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if got, want := m.perrs, wantErrors; !reflect.DeepEqual(got, want) {
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
			"https://a": {},
			"https://b": {},
		},
		now:         time.Now(),
		errs:        []string{},
		errProvider: &errProvider{},
	}
	m.Manager = Manager{
		Providers: []Provider{
			m.errProvider,
			StaticProvider([]Endpoint{
				&DOHEndpoint{Hostname: "a"},
				&DOHEndpoint{Hostname: "b"},
			}),
		},
		OnChange: func(e Endpoint) {
			m.mu.Lock()
			defer m.mu.Unlock()
			t.Logf("endpoint changed to %v", e)
			m.elected = e.String()
		},
		OnError: func(e Endpoint, err error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			t.Logf("endpoint err %v: %v", e, err)
			m.errs = append(m.errs, err.Error())
		},
		OnProviderError: func(p Provider, err error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			t.Logf("provider err %v: %v", p, err)
			m.perrs = append(m.perrs, err.Error())
		},
		testNewTransport: func(e *DOHEndpoint) http.RoundTripper {
			return m.transports[e.String()]
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
	m.wantElected(t, "https://a")
	m.wantErrors(t, []string{})
}

func TestManager_ProviderError(t *testing.T) {
	m := newTestManager(t)
	m.errProvider.err = errors.New("cannot load endpoints")

	_ = m.Test(context.Background())
	m.wantElected(t, "https://a")
	m.wantProviderErrors(t, []string{"cannot load endpoints"})
}

func TestManager_FirstFail(t *testing.T) {
	m := newTestManager(t)

	m.transports["https://a"].errs = []error{errors.New("a failed")}

	_ = m.Test(context.Background())
	m.wantElected(t, "https://b")
	m.wantErrors(t, []string{"roundtrip: a failed"})
}

func TestManager_FirstAllThenRecover(t *testing.T) {
	m := newTestManager(t)

	m.transports["https://a"].errs = []error{errors.New("a failed"), nil} // fails once then recover
	m.transports["https://b"].errs = []error{errors.New("b failed")}

	_ = m.Test(context.Background())
	m.wantElected(t, "https://a")
	m.wantErrors(t, []string{"roundtrip: a failed", "roundtrip: b failed"})
}

func TestManager_AutoRecover(t *testing.T) {
	// Fail none at init, then make enough consecutive errors to trigger a switch to second endpoint
	m := newTestManager(t)
	m.ErrorThreshold = 5

	m.transports["https://a"].errs = []error{nil, errors.New("a failed")} // succeed first req, then error
	m.transports["https://b"].errs = nil

	for i, wantElected := range []string{"https://a", "https://a", "https://a", "https://a", "https://a", "https://b", "https://b"} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			m.do()
			m.wantElected(t, wantElected)
			runtime.Gosched() // recovery happens in a goroutine
		})
	}
}

func TestManager_OpportunisticTest(t *testing.T) {
	t.SkipNow()
	// Start with first endpoint failed, then recover it to ensure the client eventually goes back to it.
	m := newTestManager(t)
	m.MinTestInterval = 2 * time.Hour

	m.transports["https://a"].errs = []error{errors.New("a failed"), nil} // fails once then recover
	m.transports["https://b"].errs = nil

	for i, wantElected := range []string{"https://b", "https://b", "https://b", "https://b", "https://a"} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			m.do()
			runtime.Gosched()
			m.wantElected(t, wantElected)
			m.addTime(35 * time.Minute)
		})
	}
}

func TestManager_Test_ContextDeadlineOnLockWait(t *testing.T) {
	m := newTestManager(t)
	m.Manager.mu.Lock()
	defer m.Manager.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := m.Test(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Test() err = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestActiveEndpoint_Test_ClearsTestingWhenManagerBlocked(t *testing.T) {
	m := newTestManager(t)
	m.BackgroundTestTimeout = 20 * time.Millisecond
	ae := &activeEnpoint{
		Endpoint:     &DOHEndpoint{Hostname: "a"},
		manager:      &m.Manager,
		lastTest:     time.Now(),
		testInterval: time.Hour,
	}

	// Force manager.Test to wait on lock until the background test context expires.
	m.Manager.mu.Lock()
	ae.test()
	time.Sleep(100 * time.Millisecond)
	m.Manager.mu.Unlock()

	if !ae.setTesting(true, false) {
		t.Fatal("testing flag is still set after background test timeout")
	}
	ae.setTesting(false, false)
}
