package endpoint

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
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

	mu             sync.RWMutex
	activeEndpoint *activeEnpoint

	testNewTransport func(e Endpoint) http.RoundTripper
	testNow          func() time.Time
}

// Test forces a test of the endpoints returned by the providers and call
// OnChange with the first healthy endpoint. If none of the provided endpoints
// are healthy, Test will continue testing endpoints until one becomes healthy
// or ctx is canceled.
func (m *Manager) Test(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.testLocked(ctx)
}

func (m *Manager) testLocked(ctx context.Context) error {
	if len(m.Providers) == 0 {
		panic("Providers is empty")
	}
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second
	for {
		ae, err := m.findBestEndpoint(ctx)
		if err == context.Canceled || err == context.DeadlineExceeded {
			return err
		}
		if ae != nil {
			// Only notify if the new best transport is different from current.
			if m.activeEndpoint == nil || m.activeEndpoint.Endpoint != ae.Endpoint {
				m.activeEndpoint = ae
				if m.OnChange != nil {
					m.mu.Unlock()
					m.OnChange(ae.Endpoint)
					m.mu.Lock()
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

func test(ctx context.Context, ae *activeEnpoint) error {
	switch ae.Protocol {
	case ProtocolDOH:
		return testDOH(ctx, ae.transport)
	case ProtocolDNS:
		return testDNS(ctx, ae.Hostname)
	default:
		panic("unsupported protocol")
	}
}

func testDNS(ctx context.Context, addr string) error {
	buf := make([]byte, 0, 514)
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{
		RecursionDesired: true,
	})
	_ = b.StartQuestions()
	_ = b.Question(dnsmessage.Question{
		Class: dnsmessage.ClassINET,
		Type:  dnsmessage.TypeA,
		Name:  dnsmessage.MustNewName(TestDomain),
	})
	buf, _ = b.Finish()
	d := &net.Dialer{}
	c, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if t, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(t)
	}
	_, err = c.Write(buf)
	if err != nil {
		return err
	}
	_, err = c.Read(buf)
	return err
}

func testDOH(ctx context.Context, rt http.RoundTripper) error {
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

func (m *Manager) findBestEndpoint(ctx context.Context) (*activeEnpoint, error) {
	var err error
	for _, p := range m.Providers {
		var endpoints []Endpoint
		endpoints, err = p.GetEndpoints(ctx)
		if err != nil {
			continue
		}
		for _, e := range endpoints {
			var ae *activeEnpoint
			if m.activeEndpoint != nil && m.activeEndpoint.Endpoint == e {
				ae = m.activeEndpoint
			} else {
				// Use current transport to test current endpoint so we benefit
				// from its already establish connection pool.
				ae = &activeEnpoint{
					Endpoint:  e,
					transport: NewTransport(e),
					manager:   m,
					lastTest:  time.Now(),
				}
				if m.testNow != nil {
					ae.lastTest = m.testNow()
				}
				if m.testNewTransport != nil {
					// Used in unit test to provide fake transport.
					ae.transport = m.testNewTransport(e)
				}
			}
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := test(ctx, ae); err != nil {
				if m.OnError != nil {
					m.OnError(e, err)
				}
				continue
			}
			return ae, nil
		}
	}
	return nil, err
}

func (m *Manager) getActiveEndpoint() (*activeEnpoint, error) {
	m.mu.RLock()
	ae := m.activeEndpoint
	m.mu.RUnlock()
	if ae == nil {
		m.mu.Lock()
		ae = m.activeEndpoint
		if ae == nil {
			// Bootstrap the active endpoint by calling a first test.
			if err := m.testLocked(context.Background()); err != nil {
				return nil, err
			}
			ae = m.activeEndpoint
		}
		m.mu.Unlock()
	}
	return ae, nil
}

func (m *Manager) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	ae, err := m.getActiveEndpoint()
	if err != nil {
		return nil, err
	}
	ae.do(func(Endpoint) error {
		resp, err = ae.transport.RoundTrip(req)
		return err
	})
	return
}

func (m *Manager) Do(ctx context.Context, action func(e Endpoint) error) error {
	ae, err := m.getActiveEndpoint()
	if err != nil {
		return err
	}
	ae.do(action)
	return nil
}

// activeEnpoint handles request successes and errors and perform opportunistic
// and recovery tests.
type activeEnpoint struct {
	Endpoint

	manager   *Manager
	transport http.RoundTripper

	mu       sync.RWMutex
	lastTest time.Time
	testing  bool

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
	minTestInterval := e.manager.MinTestInterval
	if minTestInterval == 0 {
		minTestInterval = DefaultMinTestInterval
	}
	if e.manager.testNow != nil {
		return e.manager.testNow().Sub(e.lastTest) > minTestInterval
	}
	return time.Since(e.lastTest) > minTestInterval
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
			_ = e.manager.Test(context.Background())
			e.setTesting(false)
		}()
	}
}

func (e *activeEnpoint) do(action func(e Endpoint) error) {
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
	} else {
		atomic.StoreUint32(&e.consecutiveErrors, 0)
	}
}
