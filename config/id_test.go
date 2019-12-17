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
	tests := []struct {
		name    string
		configs []string
		args    args
		want    string
	}{
		{"PrefixMatch",
			[]string{
				"10.10.10.128/27=conf1",
				"10.10.10.0/27=conf2",
				"conf3",
			},
			args{ip: net.ParseIP("10.10.10.21")},
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
