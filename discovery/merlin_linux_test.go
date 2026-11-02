package discovery

import (
	"reflect"
	"strings"
	"testing"
)

func Test_readClientList(t *testing.T) {
	// Format: <Hostname1>00:00:00:00:00:01>0>4>><Hostname2>00:00:00:00:00:02>0>24>>...
	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    map[string][]string
	}{
		{
			"Empty",
			"",
			false,
			nil,
		},
		{
			"Empty Line",
			"\n",
			false,
			map[string][]string{},
		},
		{
			"One host",
			"<foo>00:00:00:00:00:01>0>4>>",
			false,
			map[string][]string{
				"00:00:00:00:00:01": []string{"foo"},
			},
		},
		{
			"With Spaces",
			"<Foo Bar>00:00:00:00:00:01>0>4>>",
			false,
			map[string][]string{
				"00:00:00:00:00:01": []string{"Foo Bar"},
			},
		},
		{
			"Two hosts",
			"<foo>00:00:00:00:00:01>0>4>><bar>00:00:00:00:00:02>0>24>>",
			false,
			map[string][]string{
				"00:00:00:00:00:01": []string{"foo"},
				"00:00:00:00:00:02": []string{"bar"},
			},
		},
		{
			"Skip Empty Host",
			"<>00:00:00:00:00:01>0>4>><bar>00:00:00:00:00:02>0>24>>",
			false,
			map[string][]string{
				"00:00:00:00:00:02": []string{"bar"},
			},
		},
		{
			"Invalid format",
			"foo",
			true,
			nil,
		},
		{
			"Empty items",
			"<<<<<foo>00:00:00:00:00:01>0>4>>",
			true,
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readClientList([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("readClientList() Err %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("readClientList() addrs = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_readClientList_errorPrivacy(t *testing.T) {
	for _, tt := range []struct {
		name, input, reason string
	}{
		{"item separator", "PrivateClient>00:00:5e:00:53:01>0>4>>", "missing item separator"},
		{"host separator", "<PrivateClient 00:00:5e:00:53:01", "missing host separator"},
		{"MAC separator", "<PrivateClient>00:00:5e:00:53:01", "missing MAC separator"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readClientList([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("error = %v, want reason %q", err, tt.reason)
			}
			if strings.Contains(err.Error(), "PrivateClient") || strings.Contains(err.Error(), "00:00:5e:00:53:01") {
				t.Fatal("parse error includes private client data")
			}
		})
	}
}
