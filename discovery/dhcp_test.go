package discovery

import (
	"reflect"
	"strings"
	"testing"
)

func Test_readDHCPDLease(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		wantEntries map[string]string
		wantErr     bool
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
			wantEntries: map[string]string{
				"10.0.1.5":          "iPad",
				"34:42:62:2e:6c:b7": "iPad",
				"10.0.1.3":          "Mac",
				"dc:a9:04:98:2c:fe": "Mac",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := readDHCPDLease(strings.NewReader(tt.file))
			if (err != nil) != tt.wantErr {
				t.Errorf("readDHCPDLease() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(entries, tt.wantEntries) {
				t.Errorf("readDHCPDLease() entries = %v, want %v", entries, tt.wantEntries)
			}
		})
	}
}

func Test_readDNSMasqLease(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		wantEntries map[string]string
		wantErr     bool
	}{
		{
			name: "Valid file",
			file: `
56789 00:0f:66:4c:fc:c8 192.168.50.12 wrt54g 01:00:0f:66:4c:fc:c8
86400 94:83:c4:01:0b:b0 192.168.50.11 GL-MT300N-V2-bb0 *
77060 18:e8:29:af:bd:8a 192.168.50.111 ubnt *
			`,
			wantEntries: map[string]string{
				"00:0f:66:4c:fc:c8": "wrt54g",
				"192.168.50.12":     "wrt54g",
				"94:83:c4:01:0b:b0": "GL-MT300N-V2-bb0",
				"192.168.50.11":     "GL-MT300N-V2-bb0",
				"18:e8:29:af:bd:8a": "ubnt",
				"192.168.50.111":    "ubnt",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := readDNSMasqLease(strings.NewReader(tt.file))
			if (err != nil) != tt.wantErr {
				t.Errorf("readDNSMasqLease() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(entries, tt.wantEntries) {
				t.Errorf("readDNSMasqLease() entries = %v, want %v", entries, tt.wantEntries)
			}
		})
	}
}
