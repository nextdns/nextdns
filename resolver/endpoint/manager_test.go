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
	err     error
	entered chan struct{} // signaled once when GetEndpoints is entered (test sync)
	block   chan struct{} // if non-nil, GetEndpoints blocks until it is closed
}

func (e *errProvider) String() string {
	return "errProvider"
}

func (e *errProvider) GetEndpoints(ctx context.Context) ([]Endpoint, error) {
	if e.entered != nil {
		select {
		case e.entered <- struct{}{}:
		default:
		}
	}
	if e.block != nil {
		<-e.block
	}
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

// waitElected polls for the elected endpoint. The recovery test runs in a
// goroutine (activeEnpoint.test) and query reads are now lock-free, so election
// is observed eventually rather than synchronously with the triggering query.
func (m *testManager) waitElected(t *testing.T, want string) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		m.mu.Lock()
		got := m.elected
		m.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	m.mu.Lock()
	got := m.elected
	m.mu.Unlock()
	t.Errorf("Elected %v, want %v (after wait)", got, want)
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
			m.waitElected(t, wantElected) // recovery is async; reads are lock-free
		})
	}
}

// TestManager_QueryNotBlockedByBackgroundTest reproduces the wedge: an endpoint
// test holds the manager write lock across (blocked) network I/O, and a
// concurrent query must still be served from the current endpoint. On the buggy
// version getActiveEndpoint takes RLock and blocks here until the test finishes
// (up to BackgroundTestTimeout), so this test times out. With the fix (lock-free
// read) the query returns immediately.
func TestManager_QueryNotBlockedByBackgroundTest(t *testing.T) {
	m := newTestManager(t)

	// Bootstrap an active endpoint.
	if err := m.Test(context.Background()); err != nil {
		t.Fatalf("bootstrap Test() err = %v", err)
	}
	m.wantElected(t, "https://a")

	// Arrange for the next test to block inside findBestEndpointLocked (the first
	// provider's GetEndpoints) while holding m.mu.
	m.errProvider.entered = make(chan struct{}, 1)
	release := make(chan struct{})
	m.errProvider.block = release

	testReturned := make(chan struct{})
	go func() {
		_ = m.Test(context.Background())
		close(testReturned)
	}()

	// Wait until the background test is actually inside GetEndpoints, i.e. holding
	// the write lock.
	select {
	case <-m.errProvider.entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("background test never entered GetEndpoints")
	}

	// A concurrent query must not block behind the lock-holding test.
	served := make(chan error, 1)
	go func() {
		_, err := m.getActiveEndpoint(context.Background())
		served <- err
	}()
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("getActiveEndpoint() err = %v", err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		<-testReturned
		t.Fatal("query blocked while a background endpoint test held the write lock (deadlock)")
	}

	close(release)
	<-testReturned
}

// BenchmarkGetActiveEndpoint exercises the query hot path. The fix turns it into a
// single atomic load; -benchmem should report 0 allocs/op.
func BenchmarkGetActiveEndpoint(b *testing.B) {
	m := &Manager{
		Providers: []Provider{
			StaticProvider([]Endpoint{&DOHEndpoint{Hostname: "a"}}),
		},
		testNewTransport: func(e *DOHEndpoint) http.RoundTripper { return &errTransport{} },
	}
	if _, err := m.getActiveEndpoint(context.Background()); err != nil {
		b.Fatalf("bootstrap err = %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := m.getActiveEndpoint(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
	})
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

func TestManager_Test_ProbeTimeoutResetPerEndpoint(t *testing.T) {
	prevProbeTimeout := endpointProbeTimeout
	endpointProbeTimeout = 40 * time.Millisecond
	t.Cleanup(func() {
		endpointProbeTimeout = prevProbeTimeout
	})

	m := Manager{
		Providers: []Provider{
			StaticProvider([]Endpoint{
				&DOHEndpoint{Hostname: "a"},
				&DOHEndpoint{Hostname: "b"},
			}),
		},
		EndpointTester: func(e Endpoint) Tester {
			name := e.(*DOHEndpoint).Hostname
			return func(ctx context.Context, testDomain string) error {
				_ = testDomain
				deadline, ok := ctx.Deadline()
				if !ok {
					return errors.New("probe context missing deadline")
				}
				switch name {
				case "a":
					<-ctx.Done()
					return ctx.Err()
				case "b":
					if remaining := time.Until(deadline); remaining < endpointProbeTimeout/2 {
						return fmt.Errorf("probe deadline too short: %v", remaining)
					}
					return nil
				default:
					return fmt.Errorf("unexpected endpoint %q", name)
				}
			}
		},
	}

	if err := m.Test(context.Background()); err != nil {
		t.Fatalf("Test() err = %v", err)
	}
	if got := m.activeEndpoint.Load().Endpoint.String(); got != "https://b" {
		t.Fatalf("active endpoint = %s, want https://b", got)
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
