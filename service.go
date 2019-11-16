package main

import (
	"context"
	"flag"
	"fmt"
	stdlog "log"
	"math/rand"
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
	"github.com/nextdns/nextdns/proxy"
	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/endpoint"
)

var log service.Logger

type proxySvc struct {
	proxy.Proxy
	doh  *resolver.DOH
	init []func(ctx context.Context)
	stop func()
}

func (p *proxySvc) Start(s service.Service) (err error) {
	errC := make(chan error)
	go func() {
		var ctx context.Context
		ctx, p.stop = context.WithCancel(context.Background())
		defer p.stop()
		for _, f := range p.init {
			go f(ctx)
		}
		if err = p.ListenAndServe(ctx); err != nil && err != context.Canceled {
			errC <- err
		}
	}()
	select {
	case err := <-errC:
		_ = log.Errorf("Start: %v", err)
		return err
	case <-time.After(5 * time.Second):
	}
	return nil
}

func (p *proxySvc) Stop(s service.Service) error {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	return nil
}

func svc(cmd string) error {
	listen := new(string)
	conf := &cflag.Configs{}
	forwarders := &cflag.Forwarders{}
	logQueries := new(bool)
	reportClientInfo := new(bool)
	hpm := new(bool)
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
		hpm = flag.Bool("hardened-privacy", false,
			"When enabled, use DNS servers located in jurisdictions with strong privacy laws.\n"+
				"Available locations are: Switzerland, Iceland, Finland, Panama and Hong Kong.")
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
	}

	p := &proxySvc{}

	p.doh = &resolver.DOH{
		ExtraHeaders: http.Header{
			"User-Agent": []string{fmt.Sprintf("nextdns-cli/%s (%s; %s)", version, platform, runtime.GOARCH)},
		},
		Transport: nextdnsTransport(*hpm),
	}

	if len(*conf) == 0 || (len(*conf) == 1 && conf.Get(nil, nil) != "") {
		// Optimize for no dynamic configuration.
		p.doh.URL = "https://dns.nextdns.io/" + conf.Get(nil, nil)
	} else {
		p.doh.GetURL = func(q resolver.Query) string {
			return "https://dns.nextdns.io/" + conf.Get(q.PeerIP, q.MAC)
		}
	}

	p.Proxy = proxy.Proxy{
		Addr:     *listen,
		Upstream: p.doh,
		Timeout:  *timeout,
	}

	if len(*forwarders) > 0 {
		// Append default doh server at the end of the forwarder list as a catch all.
		*forwarders = append(*forwarders, cflag.Resolver{Resolver: p.doh})
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
				stdlog.Print(err)
			}
		}
	}()
	if *logQueries {
		p.QueryLog = func(q proxy.QueryInfo) {
			_ = log.Infof("%s %s %s (%d/%d) %d",
				q.PeerIP.String(),
				q.Protocol,
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
	case "start", "stop", "restart", "install", "uninstall":
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
		return nil
	case "run":
		if *reportClientInfo {
			setupClientReporting(p, conf)
		}
		return s.Run()
	default:
		panic("unknown cmd: " + cmd)
	}
}

// nextdnsTransport returns a endpoint.Manager configured to connect to NextDNS
// using different steering techniques.
func nextdnsTransport(hpm bool) http.RoundTripper {
	var qs string
	if hpm {
		qs = "?hardened_privacy=1"
	}
	return &endpoint.Manager{
		MinTestInterval: time.Second,
		Providers: []endpoint.Provider{
			// Prefer unicast routing.
			&endpoint.SourceURLProvider{
				SourceURL: "https://router.nextdns.io" + qs,
				Client: &http.Client{
					// Trick to avoid depending on DNS to contact the router API.
					Transport: endpoint.MustNew(fmt.Sprintf("https://router.nextdns.io#%s", []string{
						"216.239.32.21",
						"216.239.34.21",
						"216.239.36.21",
						"216.239.38.21",
					}[rand.Intn(3)])),
				},
			},
			// Fallback on anycast.
			endpoint.StaticProvider([]*endpoint.Endpoint{
				endpoint.MustNew("https://dns1.nextdns.io#45.90.28.0"),
				endpoint.MustNew("https://dns2.nextdns.io#45.90.30.0"),
			}),
		},
		OnError: func(e *endpoint.Endpoint, err error) {
			_ = log.Warningf("Endpoint failed: %s: %v", e, err)
		},
		OnChange: func(e *endpoint.Endpoint) {
			_ = log.Infof("Switching endpoint: %s", e)
		},
	}
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

	p.doh.ClientInfo = func(q resolver.Query) (ci resolver.ClientInfo) {
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
