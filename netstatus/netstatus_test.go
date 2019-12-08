package netstatus

import (
	"net"
	"testing"
)

func Test_diff(t *testing.T) {
	tests := []struct {
		name string
		old  []net.Interface
		new  []net.Interface
		want string
	}{
		{
			"empty",
			nil,
			nil,
			"",
		},
		{
			"new interface",
			[]net.Interface{},
			[]net.Interface{
				net.Interface{Name: "eth0"},
			},
			"eth0 added",
		},
		{
			"new interface inserted",
			[]net.Interface{
				net.Interface{Name: "lo"},
			},
			[]net.Interface{
				net.Interface{Name: "lo"},
				net.Interface{Name: "eth0"},
			},
			"eth0 added",
		},
		{
			"interface removed",
			[]net.Interface{
				net.Interface{Name: "eth0"},
			},
			[]net.Interface{},
			"eth0 removed",
		},
		{
			"interface removed head",
			[]net.Interface{
				net.Interface{Name: "eth0"},
				net.Interface{Name: "lo"},
			},
			[]net.Interface{
				net.Interface{Name: "lo"},
			},
			"eth0 removed",
		},
		{
			"interface up",
			[]net.Interface{
				net.Interface{Name: "eth0"},
			},
			[]net.Interface{
				net.Interface{Name: "eth0", Flags: net.FlagUp},
			},
			"eth0 up",
		},
		{
			"interface down",
			[]net.Interface{
				net.Interface{Name: "eth0", Flags: net.FlagUp},
			},
			[]net.Interface{
				net.Interface{Name: "eth0"},
			},
			"eth0 down",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diff(tt.old, tt.new); got != tt.want {
				t.Errorf("diff() = %v, want %v", got, tt.want)
			}
		})
	}
}

type strAddr string

func (addr strAddr) String() string {
	return string(addr)
}
func (strAddr) Network() string {
	return ""
}

func Test_diffAddrs(t *testing.T) {
	tests := []struct {
		name     string
		oldAddrs []net.Addr
		newAddrs []net.Addr
		want     string
	}{
		{
			"addr added",
			[]net.Addr{strAddr("a")},
			[]net.Addr{strAddr("a"), strAddr("b")},
			"b added",
		},
		{
			"addr removed",
			[]net.Addr{strAddr("a"), strAddr("b")},
			[]net.Addr{strAddr("a")},
			"b removed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diffAddrs(tt.oldAddrs, tt.newAddrs); got != tt.want {
				t.Errorf("diffAddrs() = %v, want %v", got, tt.want)
			}
		})
	}
}
