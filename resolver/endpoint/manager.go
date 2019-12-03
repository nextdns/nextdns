package endpoint

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var TestDomain = "probe-test.dns.nextdns.io."

const (
	// DefaultErrorThreshold defines the default value for Manager ErrorThreshold.
	DefaultErrorThreshold = 10

	// DefaultMinTestInterval defines the default value for Manager MinTestInterval.
	DefaultMinTestInterval = 2 * time.Hour

	// minTestIntervalFailed define the test interval to use when all endpoints
	// are failed.
	minTestIntervalFailed = 10 * time.Second
)

type Manager struct {
	// Providers is a list of Endpoint providers listed in order of preference.
	// The first working provided is selected on each call to Test or internal
	// test performed on error or opportunistically.
	//
	// Calling Test with an empty Providers list will result in a panic.
	Providers []Provider

	// InitEndpoint defines the endpoint to use before Providers returned a
	// working endpoint.
	InitEndpoint Endpoint

	// ErrorThreshold is the number of consecutive errors with a endpoint
	// requires to trigger a test to fallback on another endpoint. If zero,
	// DefaultErrorThreshold is used.
	ErrorThreshold int

	// MinTestInterval is the minimum interval to keep between two opportunistic
	// tests. Opportunistic tests are scheduled only when a DNS request attempt
	// is performed and the last test happened at list TestMinInterval age.
	MinTestInterval time.Duration

	// GetMinTestInterval returns the MinTestInterval to use for e. If
	// GetMinTestInterval returns 0 or is unset, MinTestInterval is used.
	GetMinTestInterval func(e Endpoint) time.Duration

	// OnChange is called whenever the active endpoint changes.
	OnChange func(e Endpoint)

	// OnConnect is called whenever an endpoint connects (for connected
	// endpoints).
	OnConnect func(*ConnectInfo)

	// OnError is called each time a test on e failed, forcing Manager to
	// fallback to the next endpoint. If e is nil, the error happended on the
	// Provider.
	OnError func(e Endpoint, err error)

	mu             sync.RWMutex
	activeEndpoint *activeEnpoint

	testNewTransport func(e *DOHEndpoint) http.RoundTripper
	testNow          func() time.Time
}

// Test forces a test of the endpoints returned by the providers and call
// OnChange with the newly selected endpoint if different.
func (m *Manager) Test(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.testLocked(ctx)
}

func (m *Manager) testLocked(ctx context.Context) {
	if len(m.Providers) == 0 {
		panic("Providers is empty")
	}
	ae := m.findBestEndpointLocked(ctx)
	// Only notify if the new best transport is different from current.
	if m.activeEndpoint == nil || !m.activeEndpoint.Endpoint.Equal(ae.Endpoint) {
		m.activeEndpoint = ae
		if m.OnChange != nil {
			m.mu.Unlock()
			m.OnChange(ae.Endpoint)
			m.mu.Lock()
		}
	}
}

// findBestEndpoint test endpoints in order and return the first healthy one. If
// not endpoint is healthy, the first available endpoint is returned, regardless
// of its health.
func (m *Manager) findBestEndpointLocked(ctx context.Context) *activeEnpoint {
	var firstEndpoint Endpoint
	for _, p := range m.Providers {
		var err error
		var endpoints []Endpoint
		endpoints, err = p.GetEndpoints(ctx)
		if err != nil {
			if m.OnError != nil {
				m.OnError(nil, err)
			}
			continue
		}
		for _, e := range endpoints {
			firstEndpoint = e
			ae := m.newActiveEndpointLocked(e)
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err = ae.Test(ctx, TestDomain); err != nil {
				if m.OnError != nil {
					m.OnError(e, err)
				}
				continue
			}
			return ae
		}
	}
	// Fallback to first endpoint with short
	ae := m.newActiveEndpointLocked(firstEndpoint)
	ae.testInterval = minTestIntervalFailed
	return ae
}

func (m *Manager) newActiveEndpointLocked(e Endpoint) (ae *activeEnpoint) {
	if m.activeEndpoint != nil && m.activeEndpoint.Endpoint.Equal(e) {
		return m.activeEndpoint
	}

	ae = &activeEnpoint{
		Endpoint: e,
		manager:  m,
		lastTest: time.Now(),
	}
	if m.GetMinTestInterval != nil {
		ae.testInterval = m.GetMinTestInterval(e)
	}
	if ae.testInterval == 0 {
		ae.testInterval = m.MinTestInterval
		if ae.testInterval == 0 {
			ae.testInterval = DefaultMinTestInterval
		}
	}
	if m.testNow != nil {
		ae.lastTest = m.testNow()
	}
	if doh, ok := e.(*DOHEndpoint); ok {
		if m.testNewTransport != nil {
			// Used in unit test to provide fake transport.
			doh.transport = m.testNewTransport(doh)
		}
		doh.onConnect = m.OnConnect
	}
	return ae
}

func (m *Manager) getActiveEndpoint() *activeEnpoint {
	m.mu.RLock()
	ae := m.activeEndpoint
	m.mu.RUnlock()
	if ae == nil {
		// Init endpoint on first call.
		m.mu.Lock()
		ae = m.activeEndpoint
		if ae == nil {
			if m.InitEndpoint != nil {
				// InitEndpoint provided, use it but zero the lastTest so an
				// async test is triggered on first query.
				ae = m.newActiveEndpointLocked(m.InitEndpoint)
				ae.lastTest = time.Time{}
			} else {
				// Bootstrap the active endpoint by calling a first test.
				m.testLocked(context.Background())
				ae = m.activeEndpoint
			}
		}
		m.mu.Unlock()
	}
	return ae
}

func (m *Manager) Do(ctx context.Context, action func(e Endpoint) error) error {
	ae := m.getActiveEndpoint()
	if ae == nil {
		return errors.New("now active endpoint")
	}
	return ae.do(action)
}

// activeEnpoint handles request successes and errors and perform opportunistic
// and recovery tests.
type activeEnpoint struct {
	Endpoint

	manager *Manager

	mu           sync.RWMutex
	lastTest     time.Time
	testInterval time.Duration
	testing      bool

	consecutiveErrors uint32
}

func (e *activeEnpoint) shouldTest() bool {
	e.mu.RLock()
	if !e.testing && e.testTimeExceededLocked() {
		e.mu.RUnlock()
		e.mu.Lock()
		should := false
		if e.testTimeExceededLocked() {
			should = true
			// Unsure only on thread wins.
			e.resetLastTestLocked()
		}
		e.mu.Unlock()
		return should
	}
	e.mu.RUnlock()
	return false
}

func (e *activeEnpoint) testTimeExceededLocked() bool {
	if e.manager.testNow != nil {
		return e.manager.testNow().Sub(e.lastTest) > e.testInterval
	}
	return time.Since(e.lastTest) > e.testInterval
}

func (e *activeEnpoint) resetLastTestLocked() {
	if e.manager.testNow != nil {
		e.lastTest = e.manager.testNow()
		return
	}
	e.lastTest = time.Now()
}

func (e *activeEnpoint) setTesting(testing bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.testing == testing {
		// Already in the target state (test in progress already).
		return false
	}
	e.testing = testing
	if !testing {
		e.resetLastTestLocked()
	}
	return true
}

func (e *activeEnpoint) test() {
	if e.setTesting(true) {
		go func() {
			e.manager.Test(context.Background())
			e.setTesting(false)
		}()
	}
}

func (e *activeEnpoint) do(action func(e Endpoint) error) error {
	if e.shouldTest() {
		// Perform an opportunistic test.
		e.test()
	}
	if err := action(e.Endpoint); err != nil {
		errThreshold := e.manager.ErrorThreshold
		if errThreshold == 0 {
			errThreshold = DefaultErrorThreshold
		}
		if atomic.AddUint32(&e.consecutiveErrors, 1) == uint32(errThreshold) {
			// Perform a recovery test.
			e.test()
		}
		return err
	}
	atomic.StoreUint32(&e.consecutiveErrors, 0)
	return nil
}
