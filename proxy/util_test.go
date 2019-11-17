package proxy

import (
	"net"
	"testing"
)

func Test_ptrIP(t *testing.T) {
	tests := []struct {
		ptr  string
		want net.IP
	}{
		{"b.a.9.8.7.6.5.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa.",
			net.ParseIP("2001:db8::567:89ab")},
		{".8.b.d.0.1.0.0.2.ip6.arpa.",
			net.ParseIP("2001:db8::")},
		{"8.16.155.10.in-addr.arpa.",
			net.ParseIP("10.155.16.8")},
		{"16.155.10.in-addr.arpa.",
			net.ParseIP("10.155.16.0")},
		{".ip6.arpa.",
			net.ParseIP("::")},
		{".in-addr.arpa.",
			net.ParseIP("0.0.0.0")},
		{"....ip6.arpa.",
			net.ParseIP("")},
		{"....in-addr.arpa.",
			net.ParseIP("")},
		{".arpa.",
			net.ParseIP("")},
	}
	for _, tt := range tests {
		t.Run(tt.ptr, func(t *testing.T) {
			if got := ptrIP(tt.ptr); !got.Equal(tt.want) {
				t.Errorf("ptrIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isPrivateReverse(t *testing.T) {
	tests := map[string]struct {
		qname string
		want  bool
	}{
		"domain":                 {"test.com", false},
		"IPv6/Public":            {"b.a.9.8.7.6.5.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa.", false},
		"IPv6/Private":           {"1.8.d.9.3.8.9.f.9.6.9.8.a.e.0.a.0.0.0.0.c.1.2.2.5.9.c.1.f.e.d.f.ip6.arpa.", true},
		"IPv6/Loopback":          {"1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.ip6.arpa.", true},
		"IPv6/AlmostLoopback":    {"2.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.ip6.arpa.", false},
		"IPv4/Public":            {"1.1.1.1.in-addr.arpa.", false},
		"IPv4/Loopback":          {"1.0.0.127.in-addr.arpa.", true},
		"IPv4/AlmostLoopback":    {"1.0.0.126.in-addr.arpa.", false},
		"IPv4/Private/10":        {"8.16.155.10.in-addr.arpa.", true},
		"IPv4/Private/192":       {"1.1.168.192.in-addr.arpa.", true},
		"IPv4/Private/Almost192": {"1.1.169.192.in-addr.arpa.", false},
		"IPv4/Private/172":       {"1.1.31.172.in-addr.arpa.", true},
		"IPv4/Private/Almost172": {"1.1.32.172.in-addr.arpa.", false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := isPrivateReverse(tt.qname); got != tt.want {
				t.Errorf("isPrivateReverse() = %v, want %v", got, tt.want)
			}
		})
	}
}
