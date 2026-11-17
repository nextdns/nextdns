package endpoint

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// http.ProxyFromEnvironment reads the environment once per process, so each
// case runs in its own child process of the test binary.
const proxyCaseEnv = "NEXTDNS_TEST_DOH_PROXY_CASE"

const (
	testBootstrapAddr = "192.0.2.1:443"
	testDoHTarget     = "example.com:443"
)

type proxyTestCase struct {
	env         map[string]string // %s is replaced by the proxy address
	auth        string            // user:pass required by the proxy
	refuse      bool              // proxy answers CONNECT with 403
	down        bool              // nothing listens on the proxy address
	wantConnect bool              // request goes through a CONNECT to testDoHTarget
	wantErr     string
}

var proxyTestCases = map[string]proxyTestCase{
	"NoEnv":            {},
	"HTTPSProxy":       {env: map[string]string{"HTTPS_PROXY": "http://%s"}, wantConnect: true},
	"LowercaseHTTPS":   {env: map[string]string{"https_proxy": "http://%s"}, wantConnect: true},
	"HTTPProxyOnly":    {env: map[string]string{"HTTP_PROXY": "http://%s", "http_proxy": "http://%s"}},
	"NoProxy":          {env: map[string]string{"HTTPS_PROXY": "http://%s", "NO_PROXY": "example.com"}},
	"LowercaseNoProxy": {env: map[string]string{"https_proxy": "http://%s", "no_proxy": "example.com"}},
	"NoProxyOther":     {env: map[string]string{"HTTPS_PROXY": "http://%s", "NO_PROXY": "example.org,10.0.0.0/8"}, wantConnect: true},
	"AuthOK":           {env: map[string]string{"HTTPS_PROXY": "http://user:secret@%s"}, auth: "user:secret", wantConnect: true},
	"AuthError":        {env: map[string]string{"HTTPS_PROXY": "http://user:wrong@%s"}, auth: "user:secret", wantConnect: true, wantErr: "Proxy Authentication Required"},
	"ConnectRefused":   {env: map[string]string{"HTTPS_PROXY": "http://%s"}, refuse: true, wantConnect: true, wantErr: "Forbidden"},
	"ProxyDown":        {env: map[string]string{"HTTPS_PROXY": "http://%s"}, down: true, wantErr: "connection refused"},
}

func TestDoHProxyRouting(t *testing.T) {
	if os.Getenv(proxyCaseEnv) != "" {
		t.Skip("child process")
	}
	var env []string
	for _, kv := range os.Environ() {
		switch strings.ToUpper(strings.SplitN(kv, "=", 2)[0]) {
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "REQUEST_METHOD":
			continue
		}
		env = append(env, kv)
	}
	for name := range proxyTestCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command(os.Args[0], "-test.run=^TestDoHProxyChild$", "-test.count=1", "-test.v")
			cmd.Env = append(append([]string(nil), env...), proxyCaseEnv+"="+name)
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "--- PASS: TestDoHProxyChild") {
				t.Fatalf("child %s: %v\n%s", name, err, out)
			}
		})
	}
}

func TestDoHProxyChild(t *testing.T) {
	name := os.Getenv(proxyCaseEnv)
	if name == "" {
		t.Skip("run by TestDoHProxyRouting")
	}
	tc, ok := proxyTestCases[name]
	if !ok {
		t.Fatalf("unknown case %q", name)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = fmt.Fprintf(w, "ok %s %s", r.Proto, r.Host)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	p := newTestConnectProxy(t, srv.Listener.Addr().String(), tc.auth, tc.refuse)
	proxyAddr := p.addr()
	if tc.down {
		p.close()
	}
	for k, v := range tc.env {
		if strings.Contains(v, "%s") {
			v = fmt.Sprintf(v, proxyAddr)
		}
		t.Setenv(k, v)
	}

	e := &DOHEndpoint{Hostname: "example.com", Path: "/abc", Bootstrap: []string{"192.0.2.1"}}
	tr := newTransport(e)
	roots := srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	var directMu sync.Mutex
	var direct []string
	for _, rt := range []http.RoundTripper{tr.RoundTripper, tr.proxied} {
		ht := rt.(roundTripperConnectTracer).RoundTripper.(*http.Transport)
		ht.TLSClientConfig.RootCAs = roots
		if ht.Proxy != nil {
			continue // keep the real dialer: it must reach the proxy
		}
		ht.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			directMu.Lock()
			direct = append(direct, addr)
			directMu.Unlock()
			var d net.Dialer
			return d.DialContext(ctx, network, srv.Listener.Addr().String())
		}
	}
	e.transport = tr
	defer e.closeTransport()

	const n = 20
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			buf := make([]byte, 512)
			l, err := e.Exchange(ctx, []byte("q"), buf)
			if err == nil && string(buf[:l]) != "ok HTTP/2.0 example.com" {
				err = fmt.Errorf("unexpected response %q", buf[:l])
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if tc.wantErr == "" && err != nil {
			t.Fatalf("Exchange: %v", err)
		}
		if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Fatalf("Exchange error = %v, want %q", err, tc.wantErr)
		}
	}

	connects := p.connects()
	directMu.Lock()
	defer directMu.Unlock()
	if tc.wantConnect {
		if len(connects) == 0 {
			t.Fatal("no CONNECT reached the proxy")
		}
		for _, c := range connects {
			if c != testDoHTarget {
				t.Fatalf("CONNECT %q, want %q", c, testDoHTarget)
			}
		}
	} else if len(connects) != 0 {
		t.Fatalf("unexpected CONNECT %v", connects)
	}
	if tc.wantConnect || tc.down {
		if len(direct) != 0 {
			t.Fatalf("proxied request bypassed the proxy: direct dials %v", direct)
		}
	} else {
		if len(direct) == 0 {
			t.Fatal("no direct dial")
		}
		for _, a := range direct {
			if a != testBootstrapAddr {
				t.Fatalf("direct dial to %q, want bootstrap %q", a, testBootstrapAddr)
			}
		}
	}

	if tc.wantConnect && tc.wantErr == "" {
		// Shutdown: closing the endpoint transport must close idle tunnels.
		// Repeat the close: a spare connection dialed for the concurrent
		// requests can still be in flight and only become idle afterwards.
		closed := p.allTunnelsClosed()
		deadline := time.After(5 * time.Second)
		for done := false; !done; {
			e.closeTransport()
			select {
			case <-closed:
				done = true
			case <-time.After(50 * time.Millisecond):
			case <-deadline:
				t.Fatal("proxy tunnels still open after closeTransport")
			}
		}
	}
}

type testConnectProxy struct {
	t        *testing.T
	ln       net.Listener
	upstream string
	auth     string
	refuse   bool

	mu      sync.Mutex
	targets []string
	open    int
	idle    chan struct{}
	wg      sync.WaitGroup
}

func newTestConnectProxy(t *testing.T, upstream, auth string, refuse bool) *testConnectProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &testConnectProxy{t: t, ln: ln, upstream: upstream, auth: auth, refuse: refuse}
	p.wg.Go(p.serve)
	t.Cleanup(func() { p.close(); p.wg.Wait() })
	return p
}

func (p *testConnectProxy) addr() string { return p.ln.Addr().String() }
func (p *testConnectProxy) close()       { _ = p.ln.Close() }

func (p *testConnectProxy) connects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.targets...)
}

func (p *testConnectProxy) allTunnelsClosed() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan struct{})
	if p.open == 0 {
		close(ch)
	} else {
		p.idle = ch
	}
	return ch
}

func (p *testConnectProxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.wg.Go(func() { p.handle(c) })
	}
}

func (p *testConnectProxy) handle(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.targets = append(p.targets, req.Host)
	p.mu.Unlock()
	switch {
	case req.Method != http.MethodConnect:
		_, _ = io.WriteString(c, "HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\n\r\n")
		return
	case p.auth != "" && req.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte(p.auth)):
		_, _ = io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"test\"\r\nContent-Length: 0\r\n\r\n")
		return
	case p.refuse:
		_, _ = io.WriteString(c, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
		return
	}
	up, err := net.Dial("tcp", p.upstream)
	if err != nil {
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer func() { _ = up.Close() }()
	p.mu.Lock()
	p.open++
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.open--
		if p.open == 0 && p.idle != nil {
			close(p.idle)
			p.idle = nil
		}
		p.mu.Unlock()
	}()
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, br); _ = up.(*net.TCPConn).CloseWrite(); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); _ = c.(*net.TCPConn).CloseWrite(); done <- struct{}{} }()
	<-done
	<-done
}
