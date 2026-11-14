package proxy

import (
	"errors"
	"testing"

	"github.com/nextdns/nextdns/resolver"
)

func TestProxy_maxInflightRequests(t *testing.T) {
	tests := []struct {
		name string
		p    Proxy
		want int
	}{
		{
			name: "default when unset",
			p:    Proxy{},
			want: defaultMaxInflightRequests,
		},
		{
			name: "configured limit",
			p:    Proxy{MaxInflightRequests: 123},
			want: 123,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.maxInflightRequests(); got != tt.want {
				t.Fatalf("maxInflightRequests() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestKeepResolved(t *testing.T) {
	upstreamErr := errors.New("upstream")
	tests := []struct {
		name string
		n    int
		info resolver.ResolveInfo
		err  error
		want bool
	}{
		{name: "fresh answer", n: 40, want: true},
		{name: "cached fallback", n: 40, info: resolver.ResolveInfo{FromCache: true}, err: upstreamErr, want: true},
		{name: "upstream failure without cache", n: 40, err: upstreamErr, want: false},
		{name: "empty cache fallback", info: resolver.ResolveInfo{FromCache: true}, err: upstreamErr, want: false},
		{name: "oversized", n: maxTCPSize + 1, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keepResolved(tt.n, tt.info, tt.err); got != tt.want {
				t.Fatalf("keepResolved() = %v, want %v", got, tt.want)
			}
		})
	}
}
