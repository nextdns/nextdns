package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/cespare/xxhash"
	"github.com/denisbrodbeck/machineid"
	"github.com/kardianos/service"

	cflag "github.com/nextdns/nextdns/flag"
	"github.com/nextdns/nextdns/mdns"
	"github.com/nextdns/nextdns/netstatus"
	"github.com/nextdns/nextdns/proxy"
	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/endpoint"
)

var log service.Logger

type proxySvc struct {
	proxy.Proxy
	resolver *resolver.DNS
	init     []func(ctx context.Context)
	stop     func()
	stopped  chan struct{}
}

func (p *proxySvc) Start(s service.Service) (err error) {
	errC := make(chan error)
	go func() {
		var ctx context.Context
		ctx, p.stop = context.WithCancel(context.Background())
		defer p.stop()
		p.stopped = make(chan struct{})
		defer close(p.stopped)
		for _, f := range p.init {
			go f(ctx)
		}
		_ = log.Infof("Starting NextDNS on %s", p.Addr)
		if err = p.ListenAndServe(ctx); err != nil && errors.Is(err, context.Canceled) {
			select {
			case errC <- err:
			default:
			}
		}
	}()
	select {
	case err := <-errC:
		_ = log.Errorf("Start error: %v", err)
		return err
	case <-time.After(5 * time.Second):
	}
	return nil
}

func (p *proxySvc) Restart() error {
	p.Stop(nil)
	return p.Start(nil)
}

func (p *proxySvc) Stop(s service.Service) error {
	if p.stop != nil {
		_ = log.Infof("Stopping NextDNS on %s", p.Addr)
		p.stop()
		p.stop = nil
		<-p.stopped
	}
	return nil
}

func svc(cmd string) error {
	listen := new(string)
	conf := &cflag.Configs{}
	forwarders := &cflag.Forwarders{}
	logQueries := new(bool)
	reportClientInfo := new(bool)
	detectCaptivePortals := new(bool)
	hpm := new(bool)
	bogusPriv := new(bool)
	timeout := new(time.Duration)
	if cmd == "run" || cmd == "install" {
		configFile := flag.String("config-file", "/etc/nextdns.conf", "Path to configuration file.")
		listen = flag.String("listen", "localhost:53", "Listen address for UDP DNS proxy server.")
		conf = cflag.Config("config", "NextDNS custom configuration id.\n"+
			"\n"+
			"The configuration id can be prefixed with a condition that is match for each query:\n"+
			"* 10.0.3.0/24=abcdef: A CIDR can be used to restrict a configuration to a subnet.\n"+
			"* 00:1c:42:2e:60:4a=abcdef: A MAC address can be used to restrict configuration\n"+
			" to a specific host on the LAN.\n"+
			"\n"+
			"This parameter can be repeated. The first match wins.")
		forwarders = cflag.Forwarder("forwarder", "A DNS server to use for a specified domain.\n"+
			"\n"+
			"Forwarders can be defined to send proxy DNS traffic to an alternative DNS upstream\n"+
			"resolver for specific domains. The format of this parameter is \n"+
			"[DOMAIN=]SERVER_ADDR[,SERVER_ADDR...].\n"+
			"\n"+
			"A SERVER_ADDR can ben either an IP for DNS53 (unencrypted UDP, TCP), or a https URL\n"+
			"for a DNS over HTTPS server. For DoH, a bootstrap IP can be specified as follow:\n"+
			"https://dns.nextdns.io#45.90.28.0. Several servers can be specified, separated by\n"+
			"comas to implement failover."+
			"\n"+
			"This parameter can be repeated. The first match wins.")
		logQueries = flag.Bool("log-queries", false, "Log DNS query.")
		reportClientInfo = flag.Bool("report-client-info", false, "Embed clients information with queries.")
		detectCaptivePortals = flag.Bool("detect-captive-portals", false,
			"Automatic detection of captive portals and fallback on system DNS to allow the connection.\n"+
				"\n"+
				"Beware that enabling this feature can allow an attacker to force nextdns to disable DoH\n"+
				"and leak unencrypted DNS traffic.")
		hpm = flag.Bool("hardened-privacy", false,
			"When enabled, use DNS servers located in jurisdictions with strong privacy laws.\n"+
				"Available locations are: Switzerland, Iceland, Finland, Panama and Hong Kong.")
		bogusPriv = flag.Bool("bogus-priv", true, "Bogus private reverse lookups.\n"+
			"\n"+
			"All reverse lookups for private IP ranges (ie 192.168.x.x, etc.) are answered with\n"+
			"\"no such domain\" rather than being forwarded upstream. The set of prefixes affected\n"+
			"is the list given in RFC6303, for IPv4 and IPv6.")
		timeout = flag.Duration("timeout", 5*time.Second, "Maximum duration allowed for a request before failing")
		flag.Parse()
		cflag.ParseFile(*configFile)
		if len(flag.Args()) > 0 {
			fmt.Printf("Unrecognized parameter: %v\n", flag.Args()[0])
			os.Exit(1)
		}
	}

	svcConfig := &service.Config{
		Name:        "nextdns",
		DisplayName: "NextDNS Proxy",
		Description: "NextDNS DNS53 to DoH proxy.",
		Arguments:   append([]string{"run"}, os.Args[1:]...),
		Dependencies: []string{
			"After=network.target",
			"Before=nss-lookup.target",
			"Wants=nss-lookup.target",
		},
	}

	p := &proxySvc{}

	p.resolver = &resolver.DNS{
		DOH: resolver.DOH{
			ExtraHeaders: http.Header{
				"User-Agent": []string{fmt.Sprintf("nextdns-cli/%s (%s; %s)", version, platform, runtime.GOARCH)},
			},
		},
		Manager: nextdnsEndpointManager(*hpm, *detectCaptivePortals),
	}

	if len(*conf) == 0 || (len(*conf) == 1 && conf.Get(nil, nil) != "") {
		// Optimize for no dynamic configuration.
		p.resolver.DOH.URL = "https://dns.nextdns.io/" + conf.Get(nil, nil)
	} else {
		p.resolver.DOH.GetURL = func(q resolver.Query) string {
			return "https://dns.nextdns.io/" + conf.Get(q.PeerIP, q.MAC)
		}
	}

	p.Proxy = proxy.Proxy{
		Addr:      *listen,
		Upstream:  p.resolver,
		BogusPriv: *bogusPriv,
		Timeout:   *timeout,
	}

	if len(*forwarders) > 0 {
		// Append default doh server at the end of the forwarder list as a catch all.
		*forwarders = append(*forwarders, cflag.Resolver{Resolver: p.resolver})
		p.Upstream = forwarders
	}

	s, err := service.New(p, svcConfig)
	if err != nil {
		stdlog.Fatal(err)
	}
	errs := make(chan error, 5)
	if log, err = s.Logger(errs); err != nil {
		stdlog.Fatal(err)
	}
	go func() {
		for {
			err := <-errs
			if err != nil {
				stdlog.Printf("System logger error: %v", err)
			}
		}
	}()
	if *logQueries {
		p.QueryLog = func(q proxy.QueryInfo) {
			_ = log.Infof("Query %s %s %s %s (qry=%d/res=%d) %dms",
				q.PeerIP.String(),
				q.Protocol,
				q.Type,
				q.Name,
				q.QuerySize,
				q.ResponseSize,
				q.Duration/time.Millisecond)
		}
	}
	p.ErrorLog = func(err error) {
		_ = log.Error(err)
	}
	switch cmd {
	case "install":
		_ = service.Control(s, "stop")
		_ = service.Control(s, "uninstall")
		err := service.Control(s, "install")
		if err == nil {
			err = service.Control(s, "start")
		}
		return err
	case "uninstall":
		_ = deactivate("")
		_ = service.Control(s, "stop")
		return service.Control(s, "uninstall")
	case "start", "stop", "restart":
		return service.Control(s, cmd)
	case "status":
		status := "unknown"
		s, err := s.Status()
		if err != nil {
			return err
		}
		switch s {
		case service.StatusRunning:
			status = "running"
		case service.StatusStopped:
			status = "stopped"
		}
		fmt.Println(status)
		if status != "running" {
			os.Exit(1)
		}
		return nil
	case "run":
		if *reportClientInfo {
			setupClientReporting(p, conf)
		}
		go func() {
			netChange := make(chan netstatus.Change)
			netstatus.Notify(netChange)
			for range netChange {
				_ = log.Info("Network change detected, restarting")
				if err := p.Restart(); err != nil {
					_ = log.Errorf("Restart failed: %v", err)
				}
			}
		}()
		return s.Run()
	default:
		panic("unknown cmd: " + cmd)
	}
}

// nextdnsEndpointManager returns a endpoint.Manager configured to connect to
// NextDNS using different steering techniques.
func nextdnsEndpointManager(hpm, captiveFallback bool) *endpoint.Manager {
	qs := "?stack=dual"
	if hpm {
		qs = "&hardened_privacy=1"
	}
	m := &endpoint.Manager{
		Providers: []endpoint.Provider{
			// Prefer unicast routing.
			&endpoint.SourceURLProvider{
				SourceURL: "https://router.nextdns.io" + qs,
				Client: &http.Client{
					// Trick to avoid depending on DNS to contact the router API.
					Transport: &endpoint.DOHEndpoint{Hostname: "router.nextdns.io", Bootstrap: []string{
						"216.239.32.21",
						"216.239.34.21",
						"216.239.36.21",
						"216.239.38.21",
					}},
				},
			},
			// Fallback on anycast.
			endpoint.StaticProvider([]endpoint.Endpoint{
				endpoint.MustNew("https://dns1.nextdns.io#45.90.28.0,2a07:a8c0::"),
				endpoint.MustNew("https://dns2.nextdns.io#45.90.30.0,2a07:a8c1::"),
			}),
		},
		InitEndpoint: endpoint.MustNew("https://dns1.nextdns.io#45.90.28.0,2a07:a8c0::"),
		OnError: func(e endpoint.Endpoint, err error) {
			_ = log.Warningf("Endpoint failed: %s: %v", e, err)
		},
		OnConnect: func(ci *endpoint.ConnectInfo) {
			for addr, dur := range ci.ConnectTimes {
				_ = log.Infof("Server %s %dms", addr, dur/time.Millisecond)
			}
			_ = log.Infof("Connected %s con=%dms tls=%dms, %s)",
				ci.ServerAddr,
				ci.ConnectTimes[ci.ServerAddr]/time.Millisecond,
				ci.TLSTime/time.Millisecond,
				ci.TLSVersion)
		},
		OnChange: func(e endpoint.Endpoint) {
			_ = log.Infof("Switching endpoint: %s", e)
		},
	}
	if captiveFallback {
		// Fallback on system DNS and set a short min test interval for when
		// plain DNS protocol is used so we go back on safe safe DoH as soon as
		// possible. This allows automatic handling of captive portals.
		m.Providers = append(m.Providers, endpoint.SystemDNSProvider{})
		m.GetMinTestInterval = func(e endpoint.Endpoint) time.Duration {
			if e.Protocol() == endpoint.ProtocolDNS {
				return 5 * time.Second
			}
			return 0 // use default MinTestInterval
		}
	}
	return m
}

func setupClientReporting(p *proxySvc, conf *cflag.Configs) {
	deviceName, _ := os.Hostname()
	deviceID, _ := machineid.ProtectedID("NextDNS")
	if len(deviceID) > 5 {
		// No need to be globally unique.
		deviceID = deviceID[:5]
	}

	mdns := &mdns.Resolver{}
	p.init = append(p.init, func(ctx context.Context) {
		_ = log.Info("Starting mDNS resolver")
		if err := mdns.Start(ctx); err != nil {
			_ = log.Warningf("Cannot start mDNS resolver: %v", err)
		}
	})

	p.resolver.DOH.ClientInfo = func(q resolver.Query) (ci resolver.ClientInfo) {
		if !q.PeerIP.IsLoopback() {
			// When acting as router, try to guess as much info as possible from
			// LAN client.
			ci.IP = q.PeerIP.String()
			ci.Name = mdns.Lookup(q.PeerIP)
			if q.MAC != nil {
				ci.ID = shortID(conf.Get(q.PeerIP, q.MAC), q.MAC)
				hex := q.MAC.String()
				if len(hex) >= 8 {
					// Only send the manufacturer part of the MAC.
					ci.Model = "mac:" + hex[:8]
				}
			}
			if ci.ID == "" {
				ci.ID = shortID(conf.Get(q.PeerIP, q.MAC), q.PeerIP)
			}
			return
		}

		ci.ID = deviceID
		ci.Name = deviceName
		return
	}
}

// shortID derives a non reversable 5 char long non globally unique ID from the
// the config + a device ID so device could not be tracked across configs.
func shortID(confID string, deviceID []byte) string {
	// Concat
	l := len(confID) + len(deviceID)
	if l < 13 {
		l = 13 // len(base32((1<<64)-1)) = 13
	}
	buf := make([]byte, 0, l)
	buf = append(buf, confID...)
	buf = append(buf, deviceID...)
	// Hash
	sum := xxhash.Sum64(buf)
	// Base 32
	strconv.AppendUint(buf[:0], sum, 32)
	// Trim 5
	buf = buf[:5]
	// Uppercase
	for i := range buf {
		if buf[i] >= 'a' {
			buf[i] ^= 1 << 5
		}
	}
	return string(buf)
}
