package config

import (
	"net"
	"testing"
)

func TestConfigs_Get(t *testing.T) {
	type args struct {
		ip  net.IP
		mac net.HardwareAddr
	}
	parseMAC := func(mac string) net.HardwareAddr {
		m, err := net.ParseMAC(mac)
		if err != nil {
			panic(err.Error())
		}
		return m
	}
	tests := []struct {
		name    string
		configs []string
		args    args
		want    string
	}{
		{"PrefixMatch",
			[]string{
				"10.10.10.128/27=conf1",
				"28:a0:2b:56:e9:66=conf2",
				"10.10.10.0/27=conf3",
				"conf4",
			},
			args{ip: net.ParseIP("10.10.10.21"), mac: parseMAC("84:89:ad:7c:e3:db")},
			"conf3",
		},
		{"MACMatch",
			[]string{
				"10.10.10.128/27=conf1",
				"28:a0:2b:56:e9:66=conf2",
				"10.10.10.0/27=conf3",
				"conf4",
			},
			args{ip: net.ParseIP("10.10.10.21"), mac: parseMAC("28:a0:2b:56:e9:66")},
			"conf2",
		},
		{"DefaultMatch",
			[]string{
				"10.10.10.128/27=conf1",
				"28:a0:2b:56:e9:66=conf2",
				"10.10.10.0/27=conf3",
				"conf4",
			},
			args{ip: net.ParseIP("1.2.3.4"), mac: parseMAC("28:a0:2b:56:e9:db")},
			"conf4",
		},
		{"NonLastDefault",
			[]string{
				"conf4",
				"10.10.10.128/27=conf1",
				"28:a0:2b:56:e9:66=conf2",
				"10.10.10.0/27=conf3",
			},
			args{ip: net.ParseIP("10.10.10.21"), mac: parseMAC("84:89:ad:7c:e3:db")},
			"conf3",
		},
		{"MultipleDefaults",
			[]string{
				"conf1",
				"conf2",
			},
			args{},
			"conf2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cs Configs
			for _, def := range tt.configs {
				if err := cs.Set(def); err != nil {
					t.Errorf("Configs.Set(%s) = Err %v", def, err)
				}
			}
			if got := cs.Get(tt.args.ip, tt.args.mac); got != tt.want {
				t.Errorf("Configs.Get() = %v, want %v", got, tt.want)
			}
		})
	}
}
