package endpoint

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http/httptrace"
	"sync"
	"time"
)

type ConnectInfo struct {
	Connect      bool
	ServerAddr   string
	ConnectTimes map[string]time.Duration
	TLSTime      time.Duration
	TLSVersion   string
}

type timer struct {
	start time.Time
	dur   time.Duration
}

func (t *timer) done() {
	t.dur = time.Since(t.start)
}

func withConnectInfo(ctx context.Context) (context.Context, *ConnectInfo) {
	ci := &ConnectInfo{}
	mu := &sync.Mutex{}
	connectTimes := map[string]*timer{}
	var tlsStart time.Time
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		ConnectStart: func(network, addr string) {
			mu.Lock()
			defer mu.Unlock()
			connectTimes[addr] = &timer{start: time.Now()}
		},
		ConnectDone: func(network, addr string, err error) {
			mu.Lock()
			defer mu.Unlock()
			if t := connectTimes[addr]; t != nil {
				t.done()
			}
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
		},
		TLSHandshakeDone: func(cs tls.ConnectionState, err error) {
			ci.TLSTime = time.Since(tlsStart)
			ci.TLSVersion = tlsVersion(cs.Version)
		},
		GotConn: func(hci httptrace.GotConnInfo) {
			if hci.Reused {
				return
			}
			ci.Connect = true
			if hci.Conn != nil {
				ci.ServerAddr = hci.Conn.RemoteAddr().String()
				ci.ConnectTimes = make(map[string]time.Duration, len(connectTimes))
				for addr, t := range connectTimes {
					ci.ConnectTimes[addr] = t.dur
				}
			}
		},
	}), ci
}

func tlsVersion(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS10"
	case tls.VersionTLS11:
		return "TLS11"
	case tls.VersionTLS12:
		return "TLS12"
	case tls.VersionTLS13:
		return "TLS13"
	}
	return fmt.Sprintf("TLS<%d>", v)
}
