package proxy

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/nextdns/nextdns/internal/dnsmessage"
	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

// mockResolver is a minimal Resolver that echoes back a success response
// with the same question section.
type mockResolver struct{}

func (mockResolver) Resolve(_ context.Context, q query.Query, buf []byte) (int, resolver.ResolveInfo, error) {
	var p dnsmessage.Parser
	h, err := p.Start(q.Payload)
	if err != nil {
		return 0, resolver.ResolveInfo{}, err
	}
	question, err := p.Question()
	if err != nil {
		return 0, resolver.ResolveInfo{}, err
	}
	h.Response = true
	h.RCode = dnsmessage.RCodeSuccess
	h.RecursionAvailable = true
	b := dnsmessage.NewBuilder(buf[:0], h)
	_ = b.StartQuestions()
	_ = b.Question(question)
	_ = b.StartAnswers()
	buf, _ = b.Finish()
	return len(buf), resolver.ResolveInfo{Transport: "mock"}, nil
}

// buildQuery constructs a minimal DNS query for the given domain.
func buildQuery(t *testing.T, domain string) []byte {
	t.Helper()
	buf := make([]byte, 0, 514)
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{
		ID:               0xABCD,
		RecursionDesired: true,
	})
	if err := b.StartQuestions(); err != nil {
		t.Fatalf("StartQuestions: %v", err)
	}
	if err := b.Question(dnsmessage.Question{
		Class: dnsmessage.ClassINET,
		Type:  dnsmessage.TypeA,
		Name:  dnsmessage.MustNewName(domain),
	}); err != nil {
		t.Fatalf("Question: %v", err)
	}
	payload, err := b.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return payload
}

func TestServeDoT_RoundTrip(t *testing.T) {
	tlsCfg, err := TLSCertConfig{}.LoadTLSConfig()
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}

	// Start a TCP listener on a random port.
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	tlsListener := tls.NewListener(tcp, tlsCfg)
	defer tlsListener.Close()

	var logged []QueryInfo
	var logMu sync.Mutex

	p := Proxy{
		Upstream:            mockResolver{},
		MaxInflightRequests: 10,
		QueryLog: func(qi QueryInfo) {
			logMu.Lock()
			logged = append(logged, qi)
			logMu.Unlock()
		},
	}

	inflightRequests := make(chan struct{}, p.MaxInflightRequests)
	go p.serveDoT(tlsListener, inflightRequests)

	// Connect as a DoT client.
	clientCfg := &tls.Config{InsecureSkipVerify: true}
	conn, err := tls.Dial("tcp", tcp.Addr().String(), clientCfg)
	if err != nil {
		t.Fatalf("tls.Dial: %v", err)
	}
	defer conn.Close()

	payload := buildQuery(t, "example.com.")

	// Write query using TCP wire format (2-byte length prefix).
	if err := binary.Write(conn, binary.BigEndian, uint16(len(payload))); err != nil {
		t.Fatalf("write length: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	// Read response.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var respLen uint16
	if err := binary.Read(conn, binary.BigEndian, &respLen); err != nil {
		t.Fatalf("read response length: %v", err)
	}
	if respLen == 0 {
		t.Fatal("empty response")
	}
	resp := make([]byte, respLen)
	if _, err := readFull(conn, resp); err != nil {
		t.Fatalf("read response body: %v", err)
	}

	// Parse response to verify it's a valid DNS reply.
	var parser dnsmessage.Parser
	hdr, err := parser.Start(resp)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !hdr.Response {
		t.Error("response bit not set")
	}
	if hdr.ID != 0xABCD {
		t.Errorf("response ID = %#x, want %#x", hdr.ID, 0xABCD)
	}
	if hdr.RCode != dnsmessage.RCodeSuccess {
		t.Errorf("RCode = %d, want success", hdr.RCode)
	}

	// Wait a moment for async query logging.
	time.Sleep(100 * time.Millisecond)

	logMu.Lock()
	defer logMu.Unlock()
	if len(logged) == 0 {
		t.Fatal("no query was logged")
	}
	if logged[0].Protocol != "DoT" {
		t.Errorf("logged protocol = %q, want %q", logged[0].Protocol, "DoT")
	}
	if logged[0].Name != "example.com." {
		t.Errorf("logged name = %q, want %q", logged[0].Name, "example.com.")
	}
}

// readFull reads exactly len(buf) bytes from conn.
func readFull(conn net.Conn, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		nn, err := conn.Read(buf[n:])
		n += nn
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func TestServeDoT_MultipleQueries(t *testing.T) {
	tlsCfg, err := TLSCertConfig{}.LoadTLSConfig()
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	tlsListener := tls.NewListener(tcp, tlsCfg)
	defer tlsListener.Close()

	p := Proxy{
		Upstream:            mockResolver{},
		MaxInflightRequests: 10,
	}

	inflightRequests := make(chan struct{}, p.MaxInflightRequests)
	go p.serveDoT(tlsListener, inflightRequests)

	clientCfg := &tls.Config{InsecureSkipVerify: true}
	conn, err := tls.Dial("tcp", tcp.Addr().String(), clientCfg)
	if err != nil {
		t.Fatalf("tls.Dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send multiple queries on the same connection.
	domains := []string{"one.example.com.", "two.example.com.", "three.example.com."}
	for _, domain := range domains {
		payload := buildQuery(t, domain)
		if err := binary.Write(conn, binary.BigEndian, uint16(len(payload))); err != nil {
			t.Fatalf("write %s length: %v", domain, err)
		}
		if _, err := conn.Write(payload); err != nil {
			t.Fatalf("write %s payload: %v", domain, err)
		}

		var respLen uint16
		if err := binary.Read(conn, binary.BigEndian, &respLen); err != nil {
			t.Fatalf("read %s response length: %v", domain, err)
		}
		resp := make([]byte, respLen)
		if _, err := readFull(conn, resp); err != nil {
			t.Fatalf("read %s response: %v", domain, err)
		}

		var parser dnsmessage.Parser
		hdr, err := parser.Start(resp)
		if err != nil {
			t.Fatalf("parse %s response: %v", domain, err)
		}
		if !hdr.Response {
			t.Errorf("%s: response bit not set", domain)
		}
	}
}
