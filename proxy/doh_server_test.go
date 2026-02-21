package proxy

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/nextdns/nextdns/internal/dnsmessage"
	"golang.org/x/net/http2"
)

func TestServeDoH_PostRoundTrip(t *testing.T) {
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
	go p.serveDoH(tlsListener, inflightRequests)

	// Build HTTP/2 client with TLS.
	clientTLS := &tls.Config{InsecureSkipVerify: true}
	tr := &http.Transport{
		TLSClientConfig: clientTLS,
	}
	// Enable HTTP/2.
	http2.ConfigureTransport(tr)
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	payload := buildQuery(t, "example.com.")
	url := "https://" + tcp.Addr().String() + "/dns-query"

	resp, err := client.Post(url, "application/dns-message", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/dns-message" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/dns-message")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var parser dnsmessage.Parser
	hdr, err := parser.Start(body)
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

	// Wait for async query logging.
	time.Sleep(100 * time.Millisecond)

	logMu.Lock()
	defer logMu.Unlock()
	if len(logged) == 0 {
		t.Fatal("no query was logged")
	}
	if logged[0].Protocol != "DoH" {
		t.Errorf("logged protocol = %q, want %q", logged[0].Protocol, "DoH")
	}
}

func TestServeDoH_GetRoundTrip(t *testing.T) {
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
	go p.serveDoH(tlsListener, inflightRequests)

	clientTLS := &tls.Config{InsecureSkipVerify: true}
	tr := &http.Transport{TLSClientConfig: clientTLS}
	http2.ConfigureTransport(tr)
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	payload := buildQuery(t, "get.example.com.")
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	url := "https://" + tcp.Addr().String() + "/dns-query?dns=" + encoded

	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var parser dnsmessage.Parser
	hdr, err := parser.Start(body)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !hdr.Response {
		t.Error("response bit not set")
	}
}

func TestServeDoH_WrongPath(t *testing.T) {
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
	go p.serveDoH(tlsListener, inflightRequests)

	clientTLS := &tls.Config{InsecureSkipVerify: true}
	tr := &http.Transport{TLSClientConfig: clientTLS}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	resp, err := client.Get("https://" + tcp.Addr().String() + "/wrong-path")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestServeDoH_WrongContentType(t *testing.T) {
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
	go p.serveDoH(tlsListener, inflightRequests)

	clientTLS := &tls.Config{InsecureSkipVerify: true}
	tr := &http.Transport{TLSClientConfig: clientTLS}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	url := "https://" + tcp.Addr().String() + "/dns-query"
	resp, err := client.Post(url, "text/plain", bytes.NewReader([]byte("not dns")))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnsupportedMediaType)
	}
}

func TestServeDoH_MethodNotAllowed(t *testing.T) {
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
	go p.serveDoH(tlsListener, inflightRequests)

	clientTLS := &tls.Config{InsecureSkipVerify: true}
	tr := &http.Transport{TLSClientConfig: clientTLS}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	url := "https://" + tcp.Addr().String() + "/dns-query"
	req, _ := http.NewRequest(http.MethodPut, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}
