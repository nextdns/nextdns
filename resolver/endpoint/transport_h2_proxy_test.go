package endpoint

import (
	"net/http"
	"testing"
)

func TestHTTPProxyFromEnv(t *testing.T) {
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(k, "")
	}
	if httpProxyFromEnv() {
		t.Fatal("expected no proxy with empty env")
	}

	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")
	if !httpProxyFromEnv() {
		t.Fatal("expected proxy when HTTPS_PROXY is set")
	}
}

func TestNewTransportUsesHostnameWithProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")

	tr := newTransport(&DOHEndpoint{
		Hostname:  "dns.nextdns.io",
		Bootstrap: []string{"45.90.28.0"},
	})
	if tr.addr != "dns.nextdns.io:443" {
		t.Fatalf("addr = %q, want hostname when proxy is set", tr.addr)
	}
	rt := tr.RoundTripper.(roundTripperConnectTracer).RoundTripper.(*http.Transport)
	if rt.Proxy == nil {
		t.Fatal("expected ProxyFromEnvironment to be configured")
	}
}

func TestNewTransportUsesBootstrapWithoutProxy(t *testing.T) {
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(k, "")
	}

	tr := newTransport(&DOHEndpoint{
		Hostname:  "dns.nextdns.io",
		Bootstrap: []string{"45.90.28.0"},
	})
	if tr.addr != "45.90.28.0:443" {
		t.Fatalf("addr = %q, want bootstrap address without proxy", tr.addr)
	}
}
