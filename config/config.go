package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/host/service"
)

type Config struct {
	File                 string
	Listens              []string
	Control              string
	Conf                 Configs
	Forwarders           Forwarders
	LogQueries           bool
	CacheSize            string
	CacheMaxAge          time.Duration
	MaxTTL               time.Duration
	ReportClientInfo     bool
	DiscoveryDNS         string
	DetectCaptivePortals bool
	BogusPriv            bool
	UseHosts             bool
	Timeout              time.Duration
	MaxInflightRequests  uint
	SetupRouter          bool
	AutoActivate         bool
}

func (c *Config) Parse(cmd string, args []string, useStorage bool) {
	if cmd == "" {
		cmd = os.Args[0]
	}
	fs := c.flagSet(cmd)
	fs.Parse(args, useStorage)
	if len(c.Listens) == 0 {
		if runtime.GOOS == "windows" {
			c.Listens = []string{"127.0.0.1:53"}
		} else {
			c.Listens = []string{"localhost:53"}
		}
	}
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
	fs.StringsVar(&c.Listens, "listen", "Listen address for UDP DNS proxy server.")
	fs.StringVar(&c.Control, "control", DefaultControl, "Address to the control socket.")
	fs.Var(&c.Conf, "config",
		"NextDNS custom configuration id.\n"+
			"\n"+
			"The configuration id can be prefixed with a condition that is match for\n"+
			"each query:\n"+
			"* 10.0.3.0/24=abcdef: A CIDR can be used to restrict a configuration to\n"+
			"  a subnet.\n"+
			"* 00:1c:42:2e:60:4a=abcdef: A MAC address can be used to restrict\n"+
			"  configuration to a specific host on the LAN.\n"+
			"\n"+
			"This parameter can be repeated. The first match wins.")
	fs.Var(&c.Forwarders, "forwarder",
		"A DNS server to use for a specified domain.\n"+
			"\n"+
			"Forwarders can be defined to send proxy DNS traffic to an alternative\n"+
			"DNS upstream resolver for specific domains. The format of this parameter\n"+
			"is [DOMAIN=]SERVER_ADDR[,SERVER_ADDR...].\n"+
			"\n"+
			"A SERVER_ADDR can ben either an IP[:PORT] for DNS53 (unencrypted UDP,\n"+
			"TCP), or a HTTPS URL for a DNS over HTTPS server. For DoH, a bootstrap\n"+
			"IP can be specified as follow: https://dns.nextdns.io#45.90.28.0.\n"+
			"Several servers can be specified, separated by comas to implement\n"+
			"failover."+
			"\n"+
			"This parameter can be repeated. The first match wins.")
	fs.BoolVar(&c.LogQueries, "log-queries", false, "Log DNS queries.")
	fs.StringVar(&c.CacheSize, "cache-size", "0",
		"Set the size of the cache in byte. Use 0 to disable caching. The value\n"+
			"can be expressed with unit like kB, MB, GB. The cache is automatically\n"+
			"flushed when the pointed configuration is updated.")
	fs.DurationVar(&c.CacheMaxAge, "cache-max-age", 0,
		"If set to greater than 0, a cached entry will be considered stale after\n"+
			"this duration, even if the record's TTL is higher.")
	fs.DurationVar(&c.MaxTTL, "max-ttl", 0,
		"If set to greater than 0, defines the maximum TTL value that will be\n"+
			"handed out to clients. The specified maximum TTL will be given to\n"+
			"clients instead of the true TTL value if it is lower. The true TTL\n"+
			"value is however kept in the cache to evaluate cache entries\n"+
			"freshness. This is best used in conjunction with the cache to force\n"+
			"clients not to rely on their own cache in order to pick up\n"+
			"configuration changes faster.")
	fs.BoolVar(&c.ReportClientInfo, "report-client-info", false,
		"Embed clients information with queries.")
	fs.StringVar(&c.DiscoveryDNS, "discovery-dns", "",
		"The address of a DNS server to be used to discover client names.\n"+
			"If not defined, the address learned via DHCP will be used. This setting\n"+
			"is only active if report-client-info is set to true.")
	fs.BoolVar(&c.DetectCaptivePortals, "detect-captive-portals", false,
		"Automatic detection of captive portals and fallback on system DNS to\n"+
			"allow the connection to establish.\n"+
			"\n"+
			"Beware that enabling this feature can allow an attacker to force nextdns\n"+
			"to disable DoH and leak unencrypted DNS traffic.")
	fs.BoolVar(new(bool), "hardened-privacy", false, "Deprecated.")
	fs.BoolVar(&c.BogusPriv, "bogus-priv", true,
		"Bogus private reverse lookups.\n"+
			"\n"+
			"All reverse lookups for private IP ranges (ie 192.168.x.x, etc.) are\n"+
			"answered with \"no such domain\" rather than being forwarded upstream.\n"+
			"The set of prefixes affected is the list given in RFC6303, for IPv4\n"+
			"and IPv6.")
	fs.BoolVar(&c.UseHosts, "use-hosts", true,
		"Lookup /etc/hosts before sending queries to upstream resolver.")
	fs.DurationVar(&c.Timeout, "timeout", 5*time.Second, "Maximum duration allowed for a request before failing.")
	fs.UintVar(&c.MaxInflightRequests, "max-inflight-requests", 256,
		"Maximum number of inflight requests handled by the proxy. No additional\n"+
			"requests will not be answered after this threshold is met. Increasing\n"+
			"this value can reduce latency in case of burst of requests but it can\n"+
			"also increase significantly memory usage.")
	fs.BoolVar(&c.SetupRouter, "setup-router", false,
		"Automatically configure NextDNS for a router setup.\n"+
			"Common types of router are detected to integrate gracefuly. Changes\n"+
			"applies are undone on daemon exit. The listen option is ignored when\n"+
			"this option is used.")
	fs.BoolVar(&c.AutoActivate, "auto-activate", false,
		"Run activate at startup and deactivate on exit.")
	return fs
}

type multiStringValue []string

func (s *multiStringValue) String() string {
	return fmt.Sprint(*s)
}

func (s *multiStringValue) Strings() []string {
	return *s
}

func (s *multiStringValue) Set(value string) error {
	for _, str := range *s {
		if value == str {
			return nil
		}
	}
	*s = append(*s, value)
	return nil
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

func (fs flagSet) StringsVar(p *[]string, name string, usage string) {
	f := (*multiStringValue)(p)
	if fs.flag != nil {
		fs.flag.Var(f, name, usage)
	}
	fs.storage[name] = f
}

func (fs flagSet) StringVar(p *string, name string, value string, usage string) {
	if fs.flag != nil {
		fs.flag.StringVar(p, name, value, usage)
	}
	fs.storage[name] = service.ConfigValue{Value: p, Default: value}
}

func (fs flagSet) BoolVar(p *bool, name string, value bool, usage string) {
	if fs.flag != nil {
		fs.flag.BoolVar(p, name, value, usage)
	}
	fs.storage[name] = service.ConfigFlag{Value: p, Default: value}
}

func (fs flagSet) DurationVar(p *time.Duration, name string, value time.Duration, usage string) {
	if fs.flag != nil {
		fs.flag.DurationVar(p, name, value, usage)
	}
	fs.storage[name] = service.ConfigDuration{Value: p, Default: value}
}

func (fs flagSet) UintVar(p *uint, name string, value uint, usage string) {
	if fs.flag != nil {
		fs.flag.UintVar(p, name, value, usage)
	}
	fs.storage[name] = service.ConfigUint{Value: p, Default: value}
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
