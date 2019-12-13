package config

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type Config struct {
	File                 string
	Listen               string
	Conf                 Configs
	Forwarders           Forwarders
	LogQueries           bool
	ReportClientInfo     bool
	DetectCaptivePortals bool
	HPM                  bool
	BogusPriv            bool
	Timeout              time.Duration
	AutoActivate         bool
}

func (c *Config) Parse(args []string) {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&c.File, "config-file", defaultConfPath(), "Path to configuration file.")
	fs.StringVar(&c.Listen, "listen", "localhost:53", "Listen address for UDP DNS proxy server.")
	fs.Var(&c.Conf, "config", "NextDNS custom configuration id.\n"+
		"\n"+
		"The configuration id can be prefixed with a condition that is match for each query:\n"+
		"* 10.0.3.0/24=abcdef: A CIDR can be used to restrict a configuration to a subnet.\n"+
		"* 00:1c:42:2e:60:4a=abcdef: A MAC address can be used to restrict configuration\n"+
		" to a specific host on the LAN.\n"+
		"\n"+
		"This parameter can be repeated. The first match wins.")
	fs.Var(&c.Forwarders, "forwarder", "A DNS server to use for a specified domain.\n"+
		"\n"+
		"Forwarders can be defined to send proxy DNS traffic to an alternative DNS upstream\n"+
		"resolver for specific domains. The format of this parameter is \n"+
		"[DOMAIN=]SERVER_ADDR[,SERVER_ADDR...].\n"+
		"\n"+
		"A SERVER_ADDR can ben either an IP[:PORT] for DNS53 (unencrypted UDP, TCP), or a HTTPS\n"+
		"URL for a DNS over HTTPS server. For DoH, a bootstrap IP can be specified as follow:\n"+
		"https://dns.nextdns.io#45.90.28.0. Several servers can be specified, separated by\n"+
		"comas to implement failover."+
		"\n"+
		"This parameter can be repeated. The first match wins.")
	fs.BoolVar(&c.LogQueries, "log-queries", false, "Log DNS query.")
	fs.BoolVar(&c.ReportClientInfo, "report-client-info", false, "Embed clients information with queries.")
	fs.BoolVar(&c.DetectCaptivePortals, "detect-captive-portals", false,
		"Automatic detection of captive portals and fallback on system DNS to allow the connection.\n"+
			"\n"+
			"Beware that enabling this feature can allow an attacker to force nextdns to disable DoH\n"+
			"and leak unencrypted DNS traffic.")
	fs.BoolVar(&c.HPM, "hardened-privacy", false,
		"When enabled, use DNS servers located in jurisdictions with strong privacy laws.\n"+
			"Available locations are: Switzerland, Iceland, Finland, Panama and Hong Kong.")
	fs.BoolVar(&c.BogusPriv, "bogus-priv", true, "Bogus private reverse lookups.\n"+
		"\n"+
		"All reverse lookups for private IP ranges (ie 192.168.x.x, etc.) are answered with\n"+
		"\"no such domain\" rather than being forwarded upstream. The set of prefixes affected\n"+
		"is the list given in RFC6303, for IPv4 and IPv6.")
	fs.DurationVar(&c.Timeout, "timeout", 5*time.Second, "Maximum duration allowed for a request before failing.")
	fs.BoolVar(&c.AutoActivate, "auto-activate", false, "Run activate at startup and deactivate on exit.")
	// Parse a copy of args to get the config file.
	_ = fs.Parse(append([]string{}, args...))
	_ = fs.Parse(c.read())
	_ = fs.Parse(args)
	if len(fs.Args()) > 0 {
		fmt.Printf("Unrecognized parameter: %v\n", fs.Args()[0])
		os.Exit(1)
	}

}

// read reads file and returns its content as a list of flags. Lines starting
// with a # or empty are ignored.
func (c *Config) read() []string {
	f, err := os.Open(c.File)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		panic(err.Error())
	}

	s := bufio.NewScanner(f)
	var args []string
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		arg := line
		value := ""
		if idx := strings.IndexByte(line, ' '); idx != -1 {
			arg = line[:idx]
			value = strings.TrimSpace(line[idx+1:])
		}
		if value != "" {
			// Accept yes/no as boolean values
			switch value {
			case "yes":
				value = "true"
			case "no":
				value = "false"
			}
			arg += "=" + value
		}
		args = append(args, "-"+arg)
	}
	if err := s.Err(); err != nil {
		panic(err.Error())
	}

	return args
}

// Save c to file.
func (c *Config) Save() error {
	f, err := os.Create(c.File)
	if err != nil {
		return fmt.Errorf("%s: %v", c.File, err)
	}
	defer f.Close()
	return c.Write(f)
}

func (c *Config) Write(w io.Writer) (err error) {
	var write = func(name string, value interface{}) {
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(w, "%s %v\n", name, value)
	}
	write("listen", c.Listen)
	for _, cf := range c.Conf {
		write("config", cf)
	}
	for _, r := range c.Forwarders {
		write("forwarder", r)
	}
	write("log-queries", c.LogQueries)
	write("report-client-info", c.ReportClientInfo)
	write("detect-captive-portals", c.DetectCaptivePortals)
	write("hardened-privacy", c.HPM)
	write("bogus-priv", c.BogusPriv)
	write("timeout", c.Timeout)
	write("auto-activate", c.AutoActivate)
	return
}
