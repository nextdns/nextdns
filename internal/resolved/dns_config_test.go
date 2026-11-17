package resolved

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestDNSServerSignature(t *testing.T) {
	if got, want := dbus.SignatureOf([]dnsServer{}).String(), "a(iayqs)"; got != want {
		t.Fatalf("dnsServer D-Bus signature = %q, want %q", got, want)
	}
}

func TestDNSConfigMatches(t *testing.T) {
	want := dnsServer{
		Family:  2,
		Address: []byte{127, 0, 0, 1},
		Port:    5354,
	}
	tests := []struct {
		name string
		got  []dnsServer
		want bool
	}{
		{
			name: "match",
			got:  []dnsServer{want},
			want: true,
		},
		{
			name: "empty",
		},
		{
			name: "extra server",
			got: []dnsServer{
				want,
				{Family: 2, Address: []byte{192, 0, 2, 1}, Port: 53},
			},
		},
		{
			name: "different address",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 53}, Port: 5354}},
		},
		{
			name: "different port",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 1}, Port: 53}},
		},
		{
			name: "different server name",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 1}, Port: 5354, ServerName: "example.com"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dnsConfigMatches(tt.got, want); got != tt.want {
				t.Fatalf("dnsConfigMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDNSConfigMatchesDefaultPort(t *testing.T) {
	want := dnsServer{
		Family:  2,
		Address: []byte{127, 0, 0, 1},
		Port:    53,
	}
	tests := []struct {
		name string
		got  []dnsServer
		want bool
	}{
		{
			name: "explicit port",
			got:  []dnsServer{want},
			want: true,
		},
		{
			name: "unspecified port",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 1}, Port: 0}},
			want: true,
		},
		{
			name: "different port",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 1}, Port: 5354}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dnsConfigMatches(tt.got, want); got != tt.want {
				t.Fatalf("dnsConfigMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}
