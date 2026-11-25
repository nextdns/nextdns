package config

import (
	"errors"
	"strings"
	"testing"
)

func TestMaxInflightUpstreamRequestsFlag(t *testing.T) {
	var c Config
	c.Parse("nextdns run", []string{"-max-inflight-upstream-requests", "7"}, false)
	if got := c.MaxInflightUpstreamRequests; got != 7 {
		t.Fatalf("MaxInflightUpstreamRequests = %d, want 7", got)
	}
}

func TestMaxInflightUpstreamRequestsFlagDefault(t *testing.T) {
	var c Config
	c.Parse("nextdns run", nil, false)
	if got := c.MaxInflightUpstreamRequests; got != 32 {
		t.Fatalf("MaxInflightUpstreamRequests = %d, want 32", got)
	}
}

func TestValidateMaxInflightUpstreamRequests(t *testing.T) {
	for _, tt := range []struct {
		name       string
		configured uint
		proxy      uint
		wantErr    string
	}{
		{name: "default", configured: 32, proxy: 256},
		{name: "zero proxy uses proxy default", configured: 32, proxy: 0},
		{name: "small proxy accepts half", configured: 2, proxy: 4},
		{name: "small proxy rejects default", configured: 32, proxy: 4, wantErr: "must not exceed 2"},
		{name: "configured above half is invalid", configured: 3, proxy: 4, wantErr: "must not exceed 2"},
		{name: "one proxy slot accepts one", configured: 1, proxy: 1},
		{name: "one proxy slot rejects two", configured: 2, proxy: 1, wantErr: "must not exceed 1"},
		{name: "zero is invalid", configured: 0, proxy: 256, wantErr: "greater than zero"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				MaxInflightRequests:         tt.proxy,
				MaxInflightUpstreamRequests: tt.configured,
			}
			err := c.Validate()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestWriteReturnsWriterError(t *testing.T) {
	for _, name := range []string{"debug", "listen"} {
		t.Run(name, func(t *testing.T) {
			c := Config{Listens: []string{"localhost:53"}}
			want := errors.New("write failed")
			w := failingConfigWriter{prefix: name + " ", err: want}
			if err := c.Write(w); !errors.Is(err, want) {
				t.Fatalf("Write() error = %v, want %v", err, want)
			}
		})
	}
}

type failingConfigWriter struct {
	prefix string
	err    error
}

func (w failingConfigWriter) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), w.prefix) {
		return 0, w.err
	}
	return len(p), nil
}
