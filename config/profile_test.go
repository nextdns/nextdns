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
		{"IPv4HostCondition",
			[]string{
				"10.10.10.21=profile-host-v4",
				"profile-default",
			},
			args{sourceIP: net.ParseIP("10.10.10.21")},
			"profile-host-v4",
		},
		{"IPv6HostCondition",
			[]string{
				"2001:db8::1=profile-host-v6",
				"profile-default",
			},
			args{sourceIP: net.ParseIP("2001:db8::1")},
			"profile-host-v6",
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

func TestProfiles_GetWithUser(t *testing.T) {
	var ps Profiles
	for _, def := range []string{
		"@rs=user-profile",
		"default-profile",
	} {
		if err := ps.Set(def); err != nil {
			t.Fatalf("Profiles.Set(%s) = Err %v", def, err)
		}
	}

	if got := ps.Get(net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.1"), nil); got != "default-profile" {
		t.Fatalf("Profiles.Get() = %v, want %v", got, "default-profile")
	}

	if got := ps.GetWithUser(net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.1"), nil, "rs"); got != "user-profile" {
		t.Fatalf("Profiles.GetWithUser() = %v, want %v", got, "user-profile")
	}

	if got := ps.GetWithUser(net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.1"), nil, "other"); got != "default-profile" {
		t.Fatalf("Profiles.GetWithUser() = %v, want %v", got, "default-profile")
	}
}

func TestProfiles_Set_ReplacesUserRule(t *testing.T) {
	var ps Profiles
	if err := ps.Set("@rs=profile1"); err != nil {
		t.Fatalf("Profiles.Set() = Err %v", err)
	}
	if err := ps.Set("@rs=profile2"); err != nil {
		t.Fatalf("Profiles.Set() = Err %v", err)
	}
	if got, want := len(ps), 1; got != want {
		t.Fatalf("len(Profiles) = %d, want %d", got, want)
	}
	if got := ps.GetWithUser(nil, nil, nil, "rs"); got != "profile2" {
		t.Fatalf("Profiles.GetWithUser() = %v, want %v", got, "profile2")
	}
}
