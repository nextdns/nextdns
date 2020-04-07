package config

import "testing"

func TestByteParsing(t *testing.T) {
	tests := []struct {
		in   string
		want uint64
	}{
		{"42", 42},
		{"42MB", 44040192},
		{"42mb", 44040192},
		{"42 MB", 44040192},
		{"42 mb", 44040192},
		{"42.5MB", 44564480},
		{"42.5 MB", 44564480},
		{"42M", 44040192},
		{"42m", 44040192},
		{"42 M", 44040192},
		{"42 m", 44040192},
		{"42.5M", 44564480},
		{"42.5 M", 44564480},
		{"1,234.03 MB", 1293974241},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseBytes(tt.in)
			if err != nil {
				t.Errorf("ParseBytes() Err=%v", err)
			}
			if got != tt.want {
				t.Errorf("ParseBytes() got %v, want %v", got, tt.want)
			}
		})
	}
}
