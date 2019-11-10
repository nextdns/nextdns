package endpoint

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var TestDomain = "probe-test.dns.nextdns.io"

const (
	// DefaultErrorThreshold defines the default value for Manager ErrorThreshold.
	DefaultErrorThreshold = 10

	// DefaultMinTestInterval defines the default value for Manager MinTestInterval.
	DefaultMinTestInterval = 2 * time.Hour
)

type Manager struct {
	// Providers is a list of Endpoint providers listed in order of preference.
	// The first working provided is selected on each call to Test or internal
	// test performed on error or opportunistically.
	//
	// Calling Test with an empty Providers list will result in a panic.
	Providers []Provider

	// ErrorThreshold is the number of consecutive errors with a endpoint
	// requires to trigger a test to fallback on another endpoint. If zero,
	// DefaultErrorThreshold is used.
	ErrorThreshold int

	// MinTestInterval is the minimum interval to keep between two opportunistic
	// tests. Opportunistic tests are scheduled only when a DNS request attempt
	// is performed and the last test happened at list TestMinInterval age.
	MinTestInterval time.Duration

	// OnChange is called whenever the active endpoint changes.
	OnChange func(e Endpoint)

	// OnError is called each time a test on e failed, forcing Manager to
	// fallback to the next endpoint.
	OnError func(e Endpoint, err error)

	mu               sync.RWMutex
	currentTransport *managerTransport

	testNewTransport func(e Endpoint) http.RoundTripper
	testNow          func() time.Time
}

// Test forces a test of the endpoints returned by the providers and call
// OnChange with the first healthy endpoint. If none of the provided endpoints
// are healthy, Test will continue testing endpoints until one becomes healthy
// or ctx is canceled.
func (m *Manager) Test(ctx context.Context) error {
	if m.OnChange == nil {
		panic("OnChange is not set")
	}
	if len(m.Providers) == 0 {
		panic("Providers is empty")
	}
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second
	for {
		t, err := m.findBestEndpoint(ctx)
		if err == context.Canceled || err == context.DeadlineExceeded {
			return err
		}
		if t != nil {
			// Only notify if the new best transport is different from current.
			if m.currentTransport == nil || m.currentTransport.endpoint != t.endpoint {
				m.mu.Lock()
				m.currentTransport = t
				m.mu.Unlock()
				if m.OnChange != nil {
					m.OnChange(t.endpoint)
				}
			}
			break
		}
		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff <<= 1
		}
	}
	return nil
}

func test(ctx context.Context, rt http.RoundTripper) error {
	req, _ := http.NewRequest("GET", "https://nowhere/?name="+TestDomain, nil)
	req = req.WithContext(ctx)
	res, err := rt.RoundTrip(req)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status: %d", res.StatusCode)
	}
	return nil
}

func (m *Manager) findBestEndpoint(ctx context.Context) (*managerTransport, error) {
	var err error
	for _, p := range m.Providers {
		var endpoints []Endpoint
		endpoints, err = p.GetEndpoints(ctx)
		if err != nil {
			continue
		}
		for _, e := range endpoints {
			var t *managerTransport
			if m.currentTransport != nil && m.currentTransport.endpoint == e {
				t = m.currentTransport
			} else {
				// Use current transport to test current endpoint so we benefit
				// from its already establish connection pool.
				t = &managerTransport{
					RoundTripper: NewTransport(e),
					manager:      m,
					endpoint:     e,
					lastTest:     time.Now(),
				}
				if m.testNow != nil {
					t.lastTest = m.testNow()
				}
				if m.testNewTransport != nil {
					// Used in unit test to provide fake transport.
					t.RoundTripper = m.testNewTransport(e)
				}
			}
			if err := test(ctx, t.RoundTripper); err != nil {
				if m.OnError != nil {
					m.OnError(e, err)
				}
				continue
			}
			return t, nil
		}
	}
	return nil, err
}

func (m *Manager) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.RLock()
	t := m.currentTransport
	m.mu.RUnlock()
	if t == nil {
		if err := m.Test(req.Context()); err != nil {
			return nil, err
		}
		m.mu.RLock()
		t = m.currentTransport
		m.mu.RUnlock()
	}
	return t.RoundTrip(req)
}

// managerTransport wraps a Transport and a Manager to perform opportunistic and
// recovery tests during round trips.
type managerTransport struct {
	http.RoundTripper

	manager  *Manager
	endpoint Endpoint

	mu       sync.RWMutex
	lastTest time.Time
	testing  bool

	consecutiveErrors uint32
}

func (t *managerTransport) shouldTest() bool {
	t.mu.RLock()
	if !t.testing && t.testTimeExceededLocked() {
		t.mu.RUnlock()
		t.mu.Lock()
		should := false
		if t.testTimeExceededLocked() {
			should = true
			// Unsure only on thread wins.
			t.resetLastTestLocked()
		}
		t.mu.Unlock()
		return should
	}
	t.mu.RUnlock()
	return false
}

func (t *managerTransport) testTimeExceededLocked() bool {
	minTestInterval := t.manager.MinTestInterval
	if minTestInterval == 0 {
		minTestInterval = DefaultMinTestInterval
	}
	if t.manager.testNow != nil {
		return t.manager.testNow().Sub(t.lastTest) > minTestInterval
	}
	return time.Since(t.lastTest) > minTestInterval
}

func (t *managerTransport) resetLastTestLocked() {
	if t.manager.testNow != nil {
		t.lastTest = t.manager.testNow()
		return
	}
	t.lastTest = time.Now()
}

func (t *managerTransport) setTesting(testing bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.testing == testing {
		// Already in the target state (test in progress already).
		return false
	}
	t.testing = testing
	if !testing {
		t.resetLastTestLocked()
	}
	return true
}

func (t *managerTransport) test() {
	if t.setTesting(true) {
		go func() {
			_ = t.manager.Test(context.Background())
			t.setTesting(false)
		}()
	}
}

func (t *managerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.shouldTest() {
		// Perform an opportunistic test.
		t.test()
	}
	res, err := t.RoundTripper.RoundTrip(req)
	if err != nil {
		errThreshold := t.manager.ErrorThreshold
		if errThreshold == 0 {
			errThreshold = DefaultErrorThreshold
		}
		if atomic.AddUint32(&t.consecutiveErrors, 1) == uint32(errThreshold) {
			// Perform a recovery test.
			t.test()
		}
	} else {
		atomic.StoreUint32(&t.consecutiveErrors, 0)
	}
	return res, err
}
