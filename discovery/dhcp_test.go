package discovery

import (
	"reflect"
	"strings"
	"testing"
)

func Test_readDHCPDLease(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		wantMACs  map[string][]string
		wantAddrs map[string][]string
		wantNames map[string][]string
		wantErr   bool
	}{
		{
			name: "Valid file",
			file: `
# The format of this file is documented in the dhcpd.leases(5) manual page.
# This lease file was written by isc-dhcp-4.3.5

# authoring-byte-order entry is generated, DO NOT DELETE
authoring-byte-order little-endian;

lease 10.0.1.4 {
	starts 0 2019/06/09 20:28:45;
	ends 0 2019/06/09 20:38:45;
	tstp 0 2019/06/09 20:38:45;
	cltt 0 2019/06/09 20:28:45;
	binding state free;
	hardware ethernet dc:a9:04:98:2c:fe;
	uid "\001\000\034B\245S\345";
}
lease 10.0.1.5 {
	starts 1 2020/01/06 01:56:24;
	ends 1 2020/01/06 03:56:24;
	cltt 1 2020/01/06 01:56:24;
	binding state active;
	next binding state free;
	rewind binding state free;
	hardware ethernet 34:42:62:2e:6c:b7;
	uid "\0014Bb.l\267";
	client-hostname "iPad";
}
lease 10.0.1.3 {
	starts 1 2020/01/06 02:08:32;
	ends 1 2020/01/06 04:08:32;
	cltt 1 2020/01/06 02:08:58;
	binding state active;
	next binding state free;
	rewind binding state free;
	hardware ethernet dc:a9:04:98:2c:fe;
	uid "\001\334\251\004\230,\376";
	client-hostname "Mac";
}`,
			wantMACs: map[string][]string{
				"34:42:62:2e:6c:b7": []string{"iPad."},
				"dc:a9:04:98:2c:fe": []string{"Mac."},
			},
			wantAddrs: map[string][]string{
				"10.0.1.5": []string{"iPad."},
				"10.0.1.3": []string{"Mac."},
			},
			wantNames: map[string][]string{
				"ipad.":       []string{"10.0.1.5"},
				"ipad.local.": []string{"10.0.1.5"},
				"mac.":        []string{"10.0.1.3"},
				"mac.local.":  []string{"10.0.1.3"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			macs, addrs, names, err := readDHCPDLease(strings.NewReader(tt.file))
			if (err != nil) != tt.wantErr {
				t.Errorf("readDHCPDLease() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(macs, tt.wantMACs) {
				t.Errorf("readDHCPDLease() macs = %v, want %v", macs, tt.wantMACs)
			}
			if !reflect.DeepEqual(addrs, tt.wantAddrs) {
				t.Errorf("readDHCPDLease() addrs = %v, want %v", addrs, tt.wantAddrs)
			}
			if !reflect.DeepEqual(names, tt.wantNames) {
				t.Errorf("readDHCPDLease() names = %v, want %v", names, tt.wantNames)
			}
		})
	}
}

func Test_readDNSMasqLease(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		wantMACs  map[string][]string
		wantAddrs map[string][]string
		wantNames map[string][]string
		wantErr   bool
	}{
		{
			name: "Valid file",
			file: `
56789 00:0f:66:4c:fc:c8 192.168.50.12 wrt54g 01:00:0f:66:4c:fc:c8
86400 94:83:c4:01:0b:b0 192.168.50.11 GL-MT300N-V2-bb0 *
77060 18:e8:29:af:bd:8a 192.168.50.111 ubnt *
			`,
			wantMACs: map[string][]string{
				"00:0f:66:4c:fc:c8": []string{"wrt54g."},
				"94:83:c4:01:0b:b0": []string{"GL-MT300N-V2-bb0."},
				"18:e8:29:af:bd:8a": []string{"ubnt."},
			},
			wantAddrs: map[string][]string{
				"192.168.50.12":  []string{"wrt54g."},
				"192.168.50.11":  []string{"GL-MT300N-V2-bb0."},
				"192.168.50.111": []string{"ubnt."},
			},
			wantNames: map[string][]string{
				"wrt54g.":                 []string{"192.168.50.12"},
				"wrt54g.local.":           []string{"192.168.50.12"},
				"gl-mt300n-v2-bb0.":       []string{"192.168.50.11"},
				"gl-mt300n-v2-bb0.local.": []string{"192.168.50.11"},
				"ubnt.":                   []string{"192.168.50.111"},
				"ubnt.local.":             []string{"192.168.50.111"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			macs, addrs, names, err := readDNSMasqLease(strings.NewReader(tt.file))
			if (err != nil) != tt.wantErr {
				t.Errorf("readDNSMasqLease() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(macs, tt.wantMACs) {
				t.Errorf("readDNSMasqLease() macs = %v, want %v", macs, tt.wantMACs)
			}
			if !reflect.DeepEqual(addrs, tt.wantAddrs) {
				t.Errorf("readDNSMasqLease() addrs = %v, want %v", addrs, tt.wantAddrs)
			}
			if !reflect.DeepEqual(names, tt.wantNames) {
				t.Errorf("readDNSMasqLease() names = %v, want %v", names, tt.wantNames)
			}
		})
	}
}
