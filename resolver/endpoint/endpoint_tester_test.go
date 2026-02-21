package endpoint

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestEndpointTester_RejectsNonDNSResponse(t *testing.T) {
	e := &DOHEndpoint{
		Hostname: "dns.nextdns.io",
		transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("<html>captive portal</html>")),
			}, nil
		}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := endpointTester(e)(ctx, TestDomain)
	if err == nil {
		t.Fatal("expected error for non-DNS response")
	}
	if !strings.Contains(err.Error(), "invalid response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEndpointTester_AcceptsValidDNSResponse(t *testing.T) {
	e := &DOHEndpoint{
		Hostname: "dns.nextdns.io",
		transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			reqBody, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			respBody := append([]byte(nil), reqBody...)
			if len(respBody) >= 3 {
				respBody[2] |= 0x80 // set QR bit.
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}, nil
		}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := endpointTester(e)(ctx, TestDomain); err != nil {
		t.Fatalf("endpointTester returned error: %v", err)
	}
}
