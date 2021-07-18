//+build http3

package endpoint

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
)

func newTransport(e *DOHEndpoint) transport {
	addrs := endpointAddrs(e)
	var t http.RoundTripper = newTransportH2(e, addrs)
	for _, alpn := range e.ALPN {
		switch alpn {
		case "h3-29", "h-32":
			t = &roundTripperFallback{
				main:     newTransportH3(e, addrs),
				fallback: t,
			}
		}
	}
	return transport{
		RoundTripper: t,
		hostname:     e.Hostname,
		path:         e.Path,
		addr:         addrs[0],
	}
}

type roundTripperFallback struct {
	main, fallback http.RoundTripper

	mu          sync.Mutex
	failedSince time.Time
}

func (rt *roundTripperFallback) isFailed() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.failedSince.IsZero() {
		if time.Since(rt.failedSince) > 1*time.Hour {
			rt.failedSince = time.Time{}
			return false
		}
		return true
	}
	return false
}

func (rt *roundTripperFallback) setFailed() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.failedSince = time.Now()
}

func (rt *roundTripperFallback) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.isFailed() {
		return rt.fallback.RoundTrip(req)
	}
	res, err := rt.main.RoundTrip(req)
	if err != nil {
		res, err = rt.fallback.RoundTrip(req)
		if err == nil {
			rt.setFailed()
		}
	}
	return res, err
}

func newTransportH3(e *DOHEndpoint, addrs []string) http.RoundTripper {
	t := &http3.RoundTripper{
		TLSClientConfig: &tls.Config{
			ServerName:         e.Hostname,
			RootCAs:            getRootCAs(),
			ClientSessionCache: tls.NewLRUClientSessionCache(0),
		},
		QuicConfig: &quic.Config{
			HandshakeIdleTimeout: 3 * time.Second,
		},
		Dial: func(_, _ string, tlsCfg *tls.Config, cfg *quic.Config) (quic.EarlySession, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			return quicDialParallel(ctx, addrs, tlsCfg, cfg)
		},
	}
	if e.onConnect != nil {
		innerDial := t.Dial
		t.Dial = func(network, addr string, tlsCfg *tls.Config, cfg *quic.Config) (quic.EarlySession, error) {
			started := time.Now()
			sess, err := innerDial(network, addr, tlsCfg, cfg)
			if err != nil {
				return sess, err
			}
			conDur := time.Since(started)
			go func() {
				<-sess.HandshakeComplete().Done()
				raddr := sess.RemoteAddr().String()
				connState := sess.ConnectionState()
				e.onConnect(&ConnectInfo{
					Connect:    true,
					ServerAddr: raddr,
					ConnectTimes: map[string]time.Duration{
						raddr: conDur,
					},
					TLSTime:    time.Since(started) - conDur,
					Protocol:   connState.TLS.NegotiatedProtocol,
					TLSVersion: tlsVersion(connState.TLS.Version),
				})
			}()
			return sess, err
		}
	}
	return t
}

type errorCode quic.ErrorCode

const errorNoError errorCode = 0x100

func (e errorCode) String() string {
	switch e {
	case errorNoError:
		return "H3_NO_ERROR"
	default:
		return fmt.Sprintf("unknown error code: %#x", uint16(e))
	}
}

func quicDialParallel(ctx context.Context, addrs []string, tlsCfg *tls.Config, cfg *quic.Config) (quic.EarlySession, error) {
	if len(addrs) == 1 {
		return quic.DialAddrEarlyContext(ctx, addrs[0], tlsCfg, cfg)
	}
	returned := make(chan struct{})
	defer close(returned)

	type dialResult struct {
		quic.EarlySession
		error
	}
	results := make(chan dialResult)

	racer := func(addr string) {
		sess, err := quic.DialAddrEarlyContext(ctx, addr, tlsCfg, cfg)
		select {
		case results <- dialResult{EarlySession: sess, error: err}:
		case <-returned:
			if sess != nil {
				_ = sess.CloseWithError(quic.ErrorCode(errorNoError), "")
			}
		}
	}

	for _, addr := range addrs {
		go racer(addr)
	}

	var err error
	for range addrs {
		res := <-results
		if res.error == nil {
			return res.EarlySession, nil
		}
		err = res.error
	}
	return nil, err
}
