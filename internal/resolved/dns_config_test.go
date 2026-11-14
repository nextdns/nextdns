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
		want dnsServer
		ok   bool
	}{
		{
			name: "match",
			got:  []dnsServer{want},
			want: want,
			ok:   true,
		},
		{
			name: "empty",
			want: want,
		},
		{
			name: "extra server",
			got: []dnsServer{
				want,
				{Family: 2, Address: []byte{192, 0, 2, 1}, Port: 53},
			},
			want: want,
		},
		{
			name: "different address",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 53}, Port: 5354}},
			want: want,
		},
		{
			name: "different port",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 1}, Port: 53}},
			want: want,
		},
		{
			name: "unspecified port matches default 53",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 1}, Port: 0}},
			want: dnsServer{Family: 2, Address: []byte{127, 0, 0, 1}, Port: 53},
			ok:   true,
		},
		{
			name: "unspecified port does not match non-default",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 1}, Port: 0}},
			want: want,
		},
		{
			name: "different server name",
			got:  []dnsServer{{Family: 2, Address: []byte{127, 0, 0, 1}, Port: 5354, ServerName: "example.com"}},
			want: want,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dnsConfigMatches(tt.got, tt.want); got != tt.ok {
				t.Fatalf("dnsConfigMatches() = %v, want %v", got, tt.ok)
			}
		})
	}
}
