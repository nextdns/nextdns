package endpoint

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestTransportH2_BlackholeRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, connections := newTestH2Transport(t, nil)
		if err := testH2Request(tr, time.Second); err != nil {
			t.Fatal(err)
		}
		conn := <-connections
		synctest.Wait()
		start := time.Now()
		conn.blackhole.Store(true)

		// Request cancellation must not be mistaken for connection recovery.
		for range 3 {
			if err := testH2Request(tr, time.Second); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("blackholed request: %v, want deadline exceeded", err)
			}
		}
		result := make(chan error, 1)
		go func() { result <- testH2Request(tr, 30*time.Second) }()
		synctest.Wait()
		tr.CloseIdleConnections()
		select {
		case <-conn.closed:
			t.Fatal("idle cleanup closed a connection with an active request")
		default:
		}

		// Use the production health timeouts. The fake clock advances while
		// the in-memory network and test goroutines are blocked.
		time.Sleep(15 * time.Second)
		select {
		case <-conn.closed:
			if elapsed := conn.closedAt.Sub(start); elapsed > 15*time.Second {
				t.Fatalf("connection closed after %v, want at most 15s", elapsed)
			}
		default:
			t.Fatal("health check did not close the blackholed connection")
		}
		if err := <-result; err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("active request: %v, want connection failure before its deadline", err)
		}
		select {
		case <-connections:
			t.Fatal("timed-out requests unexpectedly opened another connection")
		default:
		}
		if err := testH2Request(tr, time.Second); err != nil {
			t.Fatalf("request after health failure: %v", err)
		}
		select {
		case <-connections:
		default:
			t.Fatal("recovery did not establish a new connection")
		}
	})
}

func TestTransportH2_SlowHealthyResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		tr, connections := newTestH2Transport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
				_, _ = io.WriteString(w, "ok")
			case <-r.Context().Done():
			}
		}))
		result := make(chan error, 1)
		go func() { result <- testH2Request(tr, 30*time.Second) }()
		synctest.Wait()
		conn := <-connections
		// The handler takes longer than a failed health check would allow,
		// but the HTTP/2 server can still acknowledge PINGs.
		time.Sleep(20 * time.Second)
		select {
		case <-conn.closed:
			t.Fatal("health check closed a responsive connection")
		case err := <-result:
			t.Fatalf("request returned before the handler was released: %v", err)
		default:
		}
		close(release)
		if err := <-result; err != nil {
			t.Fatalf("slow response: %v", err)
		}
		select {
		case <-connections:
			t.Fatal("slow response unexpectedly needed a new connection")
		default:
		}
	})
}

func TestTransportH2_IdleConnectionExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, connections := newTestH2Transport(t, nil)
		if err := testH2Request(tr, time.Second); err != nil {
			t.Fatal(err)
		}
		conn := <-connections
		synctest.Wait()
		time.Sleep(89 * time.Second)
		select {
		case <-conn.closed:
			t.Fatal("idle connection closed too early")
		default:
		}
		time.Sleep(2 * time.Second)
		select {
		case <-conn.closed:
		default:
			t.Fatal("PING acknowledgments kept the idle connection alive")
		}
		if err := testH2Request(tr, time.Second); err != nil {
			t.Fatalf("request after idle eviction: %v", err)
		}
		select {
		case <-connections:
		default:
			t.Fatal("idle eviction did not require a new connection")
		}
	})
}

func testH2Request(tr http.RoundTripper, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", "https://example.test/dns-query", strings.NewReader("query"))
	if err != nil {
		return err
	}
	res, err := tr.RoundTrip(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.ProtoMajor != 2 {
		return errors.New("response did not use HTTP/2")
	}
	_, err = io.Copy(io.Discard, res.Body)
	return err
}

// Use net.Pipe so all network waits participate in synctest's fake clock.
// Only the dial and test certificate are substituted in the real transport.
func newTestH2Transport(t *testing.T, handler http.Handler) (*http.Transport, <-chan *testH2Conn) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.test"},
		DNSNames:     []string{"example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	if handler == nil {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = io.WriteString(w, "ok")
		})
	}
	server := &http.Server{
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		},
	}
	listener := &testH2Listener{conns: make(chan net.Conn), closed: make(chan struct{})}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ServeTLS(listener, "", "") }()

	rt := newTransportH2(&DOHEndpoint{Hostname: "example.test"}, nil)
	tr := rt.(roundTripperConnectTracer).RoundTripper.(*http.Transport)
	tr.TLSClientConfig.RootCAs = roots
	connections := make(chan *testH2Conn, 10)
	tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		conn := &testH2Conn{Conn: clientConn, closed: make(chan struct{})}
		select {
		case listener.conns <- serverConn:
			connections <- conn
			return deadlineConn{Conn: conn, timeout: 5 * time.Second}, nil
		case <-ctx.Done():
			clientConn.Close()
			serverConn.Close()
			return nil, ctx.Err()
		}
	}
	t.Cleanup(func() {
		tr.CloseIdleConnections()
		server.Close()
		listener.Close()
		if err := <-serverDone; !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("HTTP/2 server: %v", err)
		}
	})
	return tr, connections
}

type testH2Conn struct {
	net.Conn
	blackhole atomic.Bool
	closed    chan struct{}
	closedAt  time.Time // read only after closed is signaled
	once      sync.Once
}

func (c *testH2Conn) Write(p []byte) (int, error) {
	if c.blackhole.Load() {
		// A successful local write does not guarantee peer receipt. Drop
		// frames without returning an error, FIN, or RST to the client.
		return len(p), nil
	}
	return c.Conn.Write(p)
}

func (c *testH2Conn) Close() error {
	c.once.Do(func() {
		c.closedAt = time.Now()
		close(c.closed)
	})
	return c.Conn.Close()
}

type testH2Listener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func (l *testH2Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *testH2Listener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *testH2Listener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443} }
