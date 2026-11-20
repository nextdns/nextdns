package openwrt

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

const (
	// The line setting the default conf-dir in /etc/init.d/dnsmasq.
	initScript2305 = "\tconfig_get dnsmasqconfdir \"$cfg\" confdir \"/tmp/dnsmasq.d\"\n"
	initScript2410 = "\t# Create a dnsmasq.d dir for each instance\n\tconfig_get dnsmasqconfdir \"$cfg\" confdir \"/tmp/dnsmasq${cfg:+.$cfg}.d\"\n"
)

func Test_dnsmasqEnv_confDir(t *testing.T) {
	const anonymous = "dhcp.cfg01411c=dnsmasq\ndhcp.cfg01411c.domainneeded='1'"
	tests := []struct {
		name string
		// show is the output of `uci -X show dhcp.@dnsmasq[0]`, empty if it fails.
		show string
		// confdir is dhcp.@dnsmasq[0].confdir, empty if unset.
		confdir string
		// confdirErr is a uci error other than the option being unset.
		confdirErr error
		// initScript is /etc/init.d/dnsmasq, empty if missing.
		initScript string
		// generated is the configuration the init script generated when it
		// started dnsmasq, empty if it has not.
		generated string
		want      string
		wantErr   string
	}{
		{name: "24.10+", show: anonymous, initScript: initScript2410,
			want: "/tmp/dnsmasq.cfg01411c.d"},
		{name: "24.10+, named section", show: "dhcp.main=dnsmasq", initScript: initScript2410,
			want: "/tmp/dnsmasq.main.d"},
		{name: "23.05", show: anonymous, initScript: initScript2305,
			want: "/tmp/dnsmasq.d"},
		{name: "confdir option", show: anonymous, confdir: "/etc/dnsmasq.custom.d", initScript: initScript2410,
			want: "/etc/dnsmasq.custom.d"},
		{name: "confdir option with extensions", show: anonymous, confdir: "/etc/dnsmasq.custom.d,*.conf", initScript: initScript2305,
			want: "/etc/dnsmasq.custom.d"},
		{name: "relative confdir option", show: anonymous, confdir: "relative.d", initScript: initScript2410,
			wantErr: "not an absolute path"},
		{name: "uci fails reading the confdir option", show: anonymous, initScript: initScript2410,
			confdirErr: errors.New("uci: I/O error"),
			wantErr:    "I/O error"},
		{name: "no dnsmasq section", initScript: initScript2410,
			wantErr: "entry not found"},
		{name: "unexpected uci output", show: "dhcp.lan=dhcp", initScript: initScript2410,
			wantErr: "dnsmasq section not found"},
		{name: "no init script", show: anonymous,
			wantErr: "not exist"},
		{name: "init script with an unknown default", show: anonymous, initScript: "\tconfig_get dnsmasqconfdir \"$cfg\" confdir \"$CONFDIR/$cfg\"\n",
			wantErr: "unsupported default dnsmasq conf-dir"},
		{name: "init script without a default", show: anonymous, initScript: "#!/bin/sh /etc/rc.common\n",
			wantErr: "not found in /etc/init.d/dnsmasq"},
		{name: "started, generated config is not used: it can be older than the settings", show: anonymous, initScript: initScript2410,
			generated: "conf-dir=/tmp/dnsmasq.old.d\n",
			want:      "/tmp/dnsmasq.cfg01411c.d"},
		{name: "started, init script with an unknown default", show: anonymous, initScript: "\tconfig_get dnsmasqconfdir \"$cfg\" confdir \"$CONFDIR/$cfg\"\n",
			generated: "conf-file=/etc/dnsmasq.conf\nconf-dir=/tmp/dnsmasq.cfg01411c.d,*.conf\nuser=dnsmasq\n",
			want:      "/tmp/dnsmasq.cfg01411c.d"},
		{name: "started, no init script", show: anonymous,
			generated: "conf-dir=/tmp/dnsmasq.d\n",
			want:      "/tmp/dnsmasq.d"},
		{name: "started without conf-dir, init script with an unknown default", show: anonymous, initScript: "\tconfig_get dnsmasqconfdir \"$cfg\" confdir \"$CONFDIR/$cfg\"\n",
			generated: "conf-file=/etc/dnsmasq.conf\n",
			wantErr:   "unsupported default dnsmasq conf-dir"},
		{name: "started with a relative conf-dir, init script without a default", show: anonymous, initScript: "#!/bin/sh /etc/rc.common\n",
			generated: "conf-dir=relative.d\n",
			wantErr:   "not found in /etc/init.d/dnsmasq"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := dnsmasqEnv{
				uci: func(args ...string) (string, error) {
					switch strings.Join(args, " ") {
					case "-X show dhcp.@dnsmasq[0]":
						if tt.show != "" {
							return tt.show, nil
						}
					case "get dhcp.@dnsmasq[0].confdir":
						if tt.confdirErr != nil {
							return "", tt.confdirErr
						}
						if tt.confdir != "" {
							return tt.confdir, nil
						}
					}
					return "", fmt.Errorf("uci %s: %w", strings.Join(args, " "), errUCIEntryNotFound)
				},
				readFile: func(name string) ([]byte, error) {
					switch {
					case name == "/etc/init.d/dnsmasq" && tt.initScript != "":
						return []byte(tt.initScript), nil
					case name == "/var/etc/dnsmasq.conf.cfg01411c" && tt.generated != "":
						return []byte(tt.generated), nil
					}
					return nil, os.ErrNotExist
				},
			}
			got, err := e.confDir()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("confDir() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("confDir() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("confDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_dnsmasqEnv_started(t *testing.T) {
	tests := []struct {
		name string
		// files are the names of the files in /var/etc.
		files []string
		want  bool
	}{
		{name: "not started"},
		{name: "started", files: []string{"dnsmasq.conf.cfg01411c"}, want: true},
		{name: "started for another section", files: []string{"dnsmasq.conf.lan"}, want: true},
		{name: "temporary file of the init script is ignored", files: []string{"dnsmasq.conf.cfg01411c.1234"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := dnsmasqEnv{glob: func(pattern string) ([]string, error) {
				if pattern != "/var/etc/dnsmasq.conf.*" {
					t.Errorf("glob(%q)", pattern)
				}
				var files []string
				for _, name := range tt.files {
					files = append(files, "/var/etc/"+name)
				}
				return files, nil
			}}
			got, err := e.started()
			if err != nil {
				t.Fatalf("started() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("started() = %v, want %v", got, tt.want)
			}
		})
	}
}
