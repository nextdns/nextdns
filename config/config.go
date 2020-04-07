package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/host/service"
)

type Config struct {
	File                 string
	Listen               string
	Conf                 Configs
	Forwarders           Forwarders
	LogQueries           bool
	CacheSize            string
	ReportClientInfo     bool
	DetectCaptivePortals bool
	HPM                  bool
	BogusPriv            bool
	UseHosts             bool
	Timeout              time.Duration
	SetupRouter          bool
	AutoActivate         bool
}

func (c *Config) Parse(cmd string, args []string, useStorage bool) {
	if cmd == "" {
		cmd = os.Args[0]
	}
	fs := c.flagSet(cmd)
	fs.Parse(args, useStorage)
}

func (c *Config) Save() error {
	fs := c.flagSet("")
	cs, err := fs.storer()
	if err != nil {
		return err
	}
	return cs.SaveConfig(fs.storage)
}

func (c *Config) Write(w io.Writer) error {
	fs := c.flagSet("")
	for name, entry := range fs.storage {
		if entry, ok := entry.(service.ConfigListEntry); ok {
			for _, value := range entry.Strings() {
				fmt.Fprintf(w, "%s %s\n", name, value)
			}
			continue
		}
		fmt.Fprintf(w, "%s %s\n", name, entry.String())
	}
	return nil
}

func (c *Config) flagSet(cmd string) flagSet {
	fs := flagSet{
		config:  c,
		storage: map[string]service.ConfigEntry{},
	}
	if cmd != "" {
		fs.flag = flag.NewFlagSet(" "+cmd, flag.ExitOnError)
		fs.flag.StringVar(&c.File, "config-file", "", "Custom path to configuration file.")
	}
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
	fs.StringVar(&c.CacheSize, "cache-size", "",
		"Enables and set the size of the cache in byte. Can be expressed with unit like (kB, MB, GB).")
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
	fs.BoolVar(&c.UseHosts, "use-hosts", true, "Lookup /etc/hosts before sending queries to upstream resolver.")
	fs.DurationVar(&c.Timeout, "timeout", 5*time.Second, "Maximum duration allowed for a request before failing.")
	fs.BoolVar(&c.SetupRouter, "setup-router", false, "Automatically configure NextDNS for a router setup.\n"+
		"Common types of router are detected to integrate gracefuly. Changes applies are\n"+
		"undone on daemon exit. The listen option is ignored when this option is used.")
	fs.BoolVar(&c.AutoActivate, "auto-activate", false, "Run activate at startup and deactivate on exit.")
	return fs
}

// flagSet wraps a Config to make it interact with both flag and service.Config
// packages at the same time. This way settings can be changed via command line
// arguments and stored on disk using using the service package.
type flagSet struct {
	config  *Config
	flag    *flag.FlagSet
	storage map[string]service.ConfigEntry
}

func (fs flagSet) Parse(args []string, useStorage bool) {
	// Parse a copy of args to get the config file.
	_ = fs.flag.Parse(append([]string{}, args...))
	if useStorage || fs.config.File != "" {
		cs, err := fs.storer()
		if err != nil {
			fmt.Fprintln(fs.flag.Output(), err)
			os.Exit(2)
		}
		if err = cs.LoadConfig(fs.storage); err != nil {
			fmt.Fprintln(fs.flag.Output(), err)
			os.Exit(2)
		}
	}

	_ = fs.flag.Parse(args)
	if len(fs.flag.Args()) > 0 {
		fmt.Fprintf(fs.flag.Output(), "Unrecognized parameter: %v\n", fs.flag.Args()[0])
		fs.flag.PrintDefaults()
		os.Exit(2)
	}
}

func (fs flagSet) StringVar(p *string, name string, value string, usage string) {
	if fs.flag != nil {
		fs.flag.StringVar(p, name, value, usage)
	}
	fs.storage[name] = service.ConfigValue{Value: p}
}

func (fs flagSet) BoolVar(p *bool, name string, value bool, usage string) {
	if fs.flag != nil {
		fs.flag.BoolVar(p, name, value, usage)
	}
	fs.storage[name] = service.ConfigFlag{Value: p}
}

func (fs flagSet) DurationVar(p *time.Duration, name string, value time.Duration, usage string) {
	if fs.flag != nil {
		fs.flag.DurationVar(p, name, value, usage)
	}
	fs.storage[name] = service.ConfigDuration{Value: p}
}

func (fs flagSet) Var(value flag.Value, name string, usage string) {
	if fs.flag != nil {
		fs.flag.Var(value, name, usage)
	}
	fs.storage[name] = value
}

func (fs flagSet) storer() (service.ConfigStorer, error) {
	if file := fs.config.File; file != "" {
		// If config file is not provided, use system's default config manager.
		return service.ConfigFileStorer{File: file}, nil
	}
	return host.NewService(service.Config{Name: "nextdns"})
}
