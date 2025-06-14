package endpoint

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
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

	// TestEndpoint specifies a custom tester for e. If not defined or nil
	// returned, Test is called on e.
	EndpointTester func(e Endpoint) Tester

	// OnChange is called whenever the active endpoint changes.
	OnChange func(e Endpoint)

	// OnConnect is called whenever an endpoint connects (for connected
	// endpoints).
	OnConnect func(*ConnectInfo)

	// OnError is called each time a test on e failed, forcing Manager to
	// fallback to the next endpoint. If e is nil, the error happened on the
	// Provider.
	OnError func(e Endpoint, err error)

	// OnProviderError is called when a provider returns an error.
	OnProviderError func(p Provider, err error)

	// DebugLog is getting verbose logs if set.
	DebugLog func(msg string)

	// EnableDoH3 controls whether DoH3 endpoints are considered and tested.
	EnableDoH3 bool

	mu             sync.RWMutex
	activeEndpoint *activeEnpoint

	testNewTransport func(e *DOHEndpoint) http.RoundTripper
	testNow          func() time.Time
}

type Tester func(ctx context.Context, testDomain string) error

// Test forces a test of the endpoints returned by the providers and call
// OnChange with the newly selected endpoint if different.
func (m *Manager) Test(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.testLocked(ctx)
}

func (m *Manager) testLocked(ctx context.Context) error {
	if len(m.Providers) == 0 {
		panic("Providers is empty")
	}
	ae, err := m.findBestEndpointLocked(ctx)
	if err != nil {
		return err
	}
	// Only notify if the new best transport is different from current.
	if m.activeEndpoint == nil || !m.activeEndpoint.Endpoint.Equal(ae.Endpoint) {
		m.activeEndpoint = ae
		if m.OnChange != nil {
			m.mu.Unlock()
			m.OnChange(ae.Endpoint)
			m.mu.Lock()
		}
	}
	return nil
}

// findBestEndpoint test endpoints in order and return the first healthy one. If
// no endpoint is healthy, the first available endpoint is returned, regardless
// of its health.
func (m *Manager) findBestEndpointLocked(ctx context.Context) (*activeEnpoint, error) {
	m.debug("Finding best endpoint")
	var firstEndpoint Endpoint
	var doh3Endpoints []Endpoint
	var allEndpoints []Endpoint
	for _, p := range m.Providers {
		m.debugf("Provider %s", p)
		endpoints, err := p.GetEndpoints(ctx)
		if err != nil {
			m.debugf("Provider error: %s", err)
			if isErrNetUnreachable(err) {
				// Do not report network unreachable errors, bubble them up.
				return nil, err
			}
			if m.OnProviderError != nil {
				m.OnProviderError(p, err)
			}
			continue
		}
		for _, e := range endpoints {
			// Set DebugLog callback for all DOHEndpoint instances
			if doh, ok := e.(*DOHEndpoint); ok {
				doh.DebugLog = m.DebugLog
			}
			allEndpoints = append(allEndpoints, e)
			m.debugf("Testing endpoint %s", e)
			if firstEndpoint == nil {
				firstEndpoint = e
			}
			if m.EnableDoH3 { // Only consider DoH3 endpoints if enabled
				if doh, ok := e.(*DOHEndpoint); ok && doh.DoH3Supported {
					doh3Endpoints = append(doh3Endpoints, e)
				}
			}
		}
	}
	// Mark DoH3 support for all DOHEndpoint if enabled, before any selection or return
	if m.EnableDoH3 && len(allEndpoints) > 0 {
		if m.DebugLog != nil {
			m.DebugLog("[DoH3] (manager) Marking DoH3 support for all endpoints before selection")
		}
		m.MarkDoH3Support(allEndpoints)
	}
	// Prefer DoH3 endpoints if available and enabled, test them first
	if m.EnableDoH3 && len(doh3Endpoints) > 0 {
		m.debugf("Found %d DoH3 endpoints, testing for the fastest", len(doh3Endpoints))
		m.debugf("DoH3 endpoints: %v", doh3Endpoints)
		fastest, fastestIP, err := m.findFastestEndpointWithIP(ctx, doh3Endpoints)
		if err == nil && fastest != nil {
			m.debugf("Preferring fastest DoH3 endpoint %s (IP: %s)", fastest, fastestIP)
			if doh, ok := fastest.(*DOHEndpoint); ok && fastestIP != "" {
				if doh.FastestIP != fastestIP {
					doh.FastestIP = fastestIP
					doh.transport = nil    // Reset transport so it will be rebuilt with new FastestIP
					doh.once = sync.Once{} // Reset sync.Once to allow re-init
				}
			}
			ae := m.newActiveEndpointLocked(fastest)
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			var tester func(ctx context.Context, testDomain string) error
			if m.EndpointTester != nil {
				if t := m.EndpointTester(fastest); t != nil {
					tester = t
				}
			}
			if tester == nil {
				tester = endpointTester(fastest)
			}
			if err = tester(ctx, TestDomain); err == nil {
				m.debugf("Fastest DoH3 endpoint selected %s", fastest)
				return ae, nil
			}
			m.debugf("Fastest DoH3 endpoint failed: %s", err)
		}
	}
	// Fallback: test all endpoints as before
	m.debugf("No working DoH3 endpoints, testing all endpoints")
	for _, e := range allEndpoints {
		m.debugf("Testing endpoint %s", e)
		ae := m.newActiveEndpointLocked(e)
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var tester func(ctx context.Context, testDomain string) error
		if m.EndpointTester != nil {
			if t := m.EndpointTester(e); t != nil {
				tester = t
			}
		}
		if tester == nil {
			tester = endpointTester(e)
		}
		err := tester(ctx, TestDomain)
		if err != nil {
			m.debugf("Endpoint err %s", err)
			if isErrNetUnreachable(err) {
				// Do not report network unreachable errors, bubble them up.
				return nil, err
			}
			if m.OnError != nil {
				m.OnError(e, err)
			}
			continue
		}
		m.debugf("Endpoint selected %s", e)
		if doh, ok := e.(*DOHEndpoint); ok {
			if doh.DoH3Supported {
				m.debugf("Endpoint protocol: doh3, DoH3Supported: true, Hostname: %s, FastestIP: %s", doh.Hostname, doh.FastestIP)
			} else {
				m.debugf("Endpoint protocol: doh, DoH3Supported: false, Hostname: %s", doh.Hostname)
			}
		}
		return ae, nil
	}
	// Fallback to first endpoint with short interval
	m.debugf("Falling back to first endpoint %s", firstEndpoint)
	ae := m.newActiveEndpointLocked(firstEndpoint)
	ae.testInterval = minTestIntervalFailed
	return ae, nil
}

func isErrNetUnreachable(err error) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if sysErr, ok := err.(*os.SyscallError); ok {
			return sysErr.Err == syscall.ENETUNREACH
		}
	}
	return false
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

func (m *Manager) getActiveEndpoint() (*activeEnpoint, error) {
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
				if err := m.testLocked(context.Background()); err != nil {
					return nil, err
				}
				ae = m.activeEndpoint
			}
			m.activeEndpoint = ae
		}
		m.mu.Unlock()
	}
	return ae, nil
}

func (m *Manager) Do(ctx context.Context, action func(e Endpoint) error) error {
	ae, err := m.getActiveEndpoint()
	if err != nil {
		return err
	}
	if ae == nil {
		return errors.New("no active endpoint")
	}
	return ae.do(action)
}

func (m *Manager) debug(msg string) {
	if m.DebugLog != nil {
		m.DebugLog(msg)
	}
}

func (m *Manager) debugf(format string, a ...interface{}) {
	if m.DebugLog != nil {
		m.DebugLog(fmt.Sprintf(format, a...))
	}
}

// Check and mark DoH3 support for all DOHEndpoint in a list.
func (m *Manager) MarkDoH3Support(endpoints []Endpoint) {
	if m.DebugLog != nil {
		m.DebugLog(fmt.Sprintf("[DoH3] MarkDoH3Support called for %d endpoints", len(endpoints)))
	}
	for i, e := range endpoints {
		if m.DebugLog != nil {
			m.DebugLog(fmt.Sprintf("[DoH3] Endpoint[%d] type: %T, value: %v", i, e, e))
		}
		if doh, ok := e.(*DOHEndpoint); ok {
			if m.DebugLog != nil {
				m.DebugLog(fmt.Sprintf("[DoH3] Probing DoH3 for Hostname=%s, Bootstrap=%v, ALPN=%v, DebugLog set: %v", doh.Hostname, doh.Bootstrap, doh.ALPN, doh.DebugLog != nil))
			}
			doh.DoH3Supported = SupportsDoH3(doh.Hostname, doh.Bootstrap, doh.ALPN, doh.DebugLog)
		}
	}
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

func (e *activeEnpoint) setTesting(testing, resetTimer bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.testing == testing {
		// Already in the target state (test in progress already).
		return false
	}
	e.testing = testing
	if resetTimer {
		e.resetLastTestLocked()
	}
	return true
}

func (e *activeEnpoint) test() {
	if e.setTesting(true, false) {
		go func() {
			err := e.manager.Test(context.Background())
			reset := err == nil // do not reset test timer if test failed.
			e.setTesting(false, reset)
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

// findFastestEndpointWithIP returns the fastest endpoint and the fastest IP within its Bootstrap IPs.
func (m *Manager) findFastestEndpointWithIP(ctx context.Context, endpoints []Endpoint) (Endpoint, string, error) {
	var fastest Endpoint
	var fastestIP string
	var minLatency time.Duration
	for _, e := range endpoints {
		doh, ok := e.(*DOHEndpoint)
		if !ok || len(doh.Bootstrap) == 0 {
			continue
		}
		lat, ip, err := MeasureLatency(ctx, doh.Hostname, doh.Bootstrap)
		if err != nil {
			continue
		}
		if fastest == nil || lat < minLatency {
			fastest = e
			fastestIP = ip
			minLatency = lat
		}
	}
	if fastest == nil && len(endpoints) > 0 {
		return endpoints[0], "", nil // fallback
	}
	return fastest, fastestIP, nil
}

// StartPeriodicFastestCheck starts a goroutine that periodically re-evaluates all endpoints and switches to the fastest.
func (m *Manager) StartPeriodicFastestCheck(interval time.Duration, stopCh <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute+30*time.Second)
				_ = m.Test(ctx) // This will re-run the fastest endpoint selection
				cancel()
			case <-stopCh:
				return
			}
		}
	}()
}
