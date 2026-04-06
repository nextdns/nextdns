package openwrt

import "testing"

func Test_normalizeRouterIP(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "ipv4",
			input: "192.168.1.1",
			want:  "192.168.1.1",
		},
		{
			name:  "ipv4 cidr",
			input: "192.168.2.1/24",
			want:  "192.168.2.1",
		},
		{
			name:  "ipv6 cidr",
			input: "fd00::1/64",
			want:  "fd00::1",
		},
		{
			name:  "trim spaces",
			input: " 192.168.3.1/24 \n",
			want:  "192.168.3.1",
		},
		{
			name:    "invalid cidr",
			input:   "192.168.1.1/abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeRouterIP(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeRouterIP(%q) error = nil, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeRouterIP(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("normalizeRouterIP(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
