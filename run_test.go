package main

import (
	"strings"
	"testing"

	"github.com/nextdns/nextdns/config"
)

func Test_isLocalhostMode(t *testing.T) {
	tests := []struct {
		listens []string
		want    bool
	}{
		{[]string{"127.0.0.1:53"}, true},
		{[]string{"127.0.0.1:5353"}, true},
		{[]string{"10.0.0.1:53"}, false},
		{[]string{"127.0.0.1:53", "10.0.0.1:53"}, false},
		{[]string{"10.0.0.1:53", "127.0.0.1:53"}, false},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.listens, ","), func(t *testing.T) {
			if got := isLocalhostMode(&config.Config{Listens: tt.listens}); got != tt.want {
				t.Errorf("isLocalhostMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
