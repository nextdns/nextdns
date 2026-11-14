package synology

import (
	"bytes"
	"strings"
	"testing"
	"text/template"
)

func TestDHCPEnabled(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "exact", data: `enable="yes"`, want: true},
		{name: "with prefix", data: "foo\nenable=\"yes\"\n", want: true},
		{name: "disabled", data: `enable="no"`, want: false},
		{name: "empty", data: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bytes.Contains([]byte(tt.data), []byte(`enable="yes"`))
			if got != tt.want {
				t.Fatalf("contains enable=yes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasPortZero(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{name: "disabled", config: "port=0\n", want: true},
		{name: "whitespace", config: "  port=0  \n", want: true},
		{name: "spaces around equals", config: "port = 0\n", want: false},
		{name: "commented", config: "# port=0\n", want: false},
		{name: "different port", config: "port=53\n", want: false},
		{name: "different option", config: "server=127.0.0.1#53\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPortZero([]byte(tt.config)); got != tt.want {
				t.Fatalf("hasPortZero(%q) = %v, want %v", tt.config, got, tt.want)
			}
		})
	}
}

func TestTemplateDoesNotRepeatPort(t *testing.T) {
	tests := []struct {
		name           string
		portZeroExists bool
		wantPort       bool
	}{
		{name: "not present", wantPort: true},
		{name: "already present", portZeroExists: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Router{CacheEnabled: true, PortZeroExists: tt.portZeroExists}
			var b bytes.Buffer
			if err := template.Must(template.New("").Parse(tmpl)).Execute(&b, r); err != nil {
				t.Fatal(err)
			}

			if gotPort := strings.Contains(b.String(), "port=0"); gotPort != tt.wantPort {
				t.Fatalf("template contains port=0 = %v, want %v; output: %q", gotPort, tt.wantPort, b.String())
			}
		})
	}
}
