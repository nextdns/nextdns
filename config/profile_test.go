package config

import (
	"net"
	"testing"
)

func TestProfiles_Get(t *testing.T) {
	type args struct {
		sourceIP net.IP
		destIP   net.IP
		mac      net.HardwareAddr
	}
	parseMAC := func(mac string) net.HardwareAddr {
		m, err := net.ParseMAC(mac)
		if err != nil {
			panic(err.Error())
		}
		return m
	}
	tests := []struct {
		name     string
		profiles []string
		args     args
		want     string
	}{
		{"PrefixMatch",
			[]string{
				"10.10.10.128/27=profile1",
				"28:a0:2b:56:e9:66=profile2",
				"10.10.10.0/27=profile3",
				"profile4",
			},
			args{sourceIP: net.ParseIP("10.10.10.21"), destIP: net.ParseIP("10.10.10.1"), mac: parseMAC("84:89:ad:7c:e3:db")},
			"profile3",
		},
		{"MACMatch",
			[]string{
				"10.10.10.128/27=profile1",
				"28:a0:2b:56:e9:66=profile2",
				"10.10.10.0/27=profile3",
				"profile4",
			},
			args{sourceIP: net.ParseIP("10.10.10.21"), destIP: net.ParseIP("10.10.10.1"), mac: parseMAC("28:a0:2b:56:e9:66")},
			"profile2",
		},
		{"DefaultMatch",
			[]string{
				"10.10.10.128/27=profile1",
				"28:a0:2b:56:e9:66=profile2",
				"10.10.10.0/27=profile3",
				"profile4",
			},
			args{sourceIP: net.ParseIP("1.2.3.4"), destIP: net.ParseIP("10.10.10.1"), mac: parseMAC("28:a0:2b:56:e9:db")},
			"profile4",
		},
		{"NonLastDefault",
			[]string{
				"profile4",
				"10.10.10.128/27=profile1",
				"28:a0:2b:56:e9:66=profile2",
				"10.10.10.0/27=profile3",
			},
			args{sourceIP: net.ParseIP("10.10.10.21"), destIP: net.ParseIP("10.10.10.1"), mac: parseMAC("84:89:ad:7c:e3:db")},
			"profile3",
		},
		{"MultipleDefaults",
			[]string{
				"profile1",
				"profile2",
			},
			args{},
			"profile2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ps Profiles
			for _, def := range tt.profiles {
				if err := ps.Set(def); err != nil {
					t.Errorf("Profiles.Set(%s) = Err %v", def, err)
				}
			}
			if got := ps.Get(tt.args.sourceIP, tt.args.destIP, tt.args.mac); got != tt.want {
				t.Errorf("Profiles.Get() = %v, want %v", got, tt.want)
			}
		})
	}
}
