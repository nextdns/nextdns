package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nextdns/nextdns/hosts"

	"github.com/cespare/xxhash"
	"github.com/denisbrodbeck/machineid"
	lru "github.com/hashicorp/golang-lru"

	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/discovery"
	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/netstatus"
	"github.com/nextdns/nextdns/proxy"
	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/endpoint"
	"github.com/nextdns/nextdns/resolver/query"
	"github.com/nextdns/nextdns/router"
)

type proxySvc struct {
	proxy.Proxy
	log      host.Logger
	resolver *resolver.DNS
	stopFunc func()
	stopped  chan struct{}

	// OnInit is called every time the proxy is started or restarted. The ctx is
	// cancelled on stop or restart.
	OnInit []func(ctx context.Context)

	// OnStarted is called once the daemon is fully started.
	OnStarted []func()

	// OnStopped is called once the daemon is full stopped.
	OnStopped []func()
}

func (p *proxySvc) Start() (err error) {
	p.log.Infof("Starting NextDNS %s/%s on %s", version, platform, p.Addr)
	backoff := 100 * time.Millisecond
	for {
		if err = p.start(); err != nil {
			if isErrNetUnreachable(err) {
				p.log.Infof("Network not yet ready, waiting")
				time.Sleep(backoff)
				backoff <<= 1
				continue
			}
			return err
		}
		break
	}
	for _, f := range p.OnStarted {
		f()
	}
	return nil
}

func isErrNetUnreachable(err error) bool {
	if strings.Contains(err.Error(), "network is unreachable") {
		return true
	}
	for ; err != nil; err = errors.Unwrap(err) {
		if sysErr, ok := err.(*os.SyscallError); ok {
			return sysErr.Err == syscall.ENETUNREACH
		}
	}
	return false
}

func (p *proxySvc) start() (err error) {
	errC := make(chan error)
	var ctx context.Context
	go func() {
		ctx, p.stopFunc = context.WithCancel(context.Background())
		defer p.stopFunc()
		p.stopped = make(chan struct{})
		defer close(p.stopped)
		for _, f := range p.OnInit {
			go f(ctx)
		}
		if err = p.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
			select {
			case errC <- err:
			default:
			}
		}
	}()
	select {
	case err := <-errC:
		return err
	case <-time.After(5 * time.Second):
	}
	return nil
}

func (p *proxySvc) Restart() error {
	p.log.Infof("Restarting NextDNS %s/%s on %s", version, platform, p.Addr)
	_ = p.stop()
	return p.start()
}

func (p *proxySvc) Stop() error {
	p.log.Infof("Stopping NextDNS %s/%s", version, platform)
	if p.stop() {
		for _, f := range p.OnStopped {
			f()
		}
	}
	p.log.Infof("NextDNS %s/%s stopped", version, platform)
	return nil
}

func (p *proxySvc) stop() bool {
	if p.stopFunc == nil {
		return false
	}
	p.stopFunc()
	p.stopFunc = nil
	<-p.stopped
	return true
}

func (p *proxySvc) Log(msg string) {
	p.log.Info(msg)
}

func run(args []string) error {
	cmd := args[0]
	args = args[1:]
	var c config.Config
	// When running interactive, ignore config file unless explicitely specified.
	useStorage := service.CurrentRunMode() == service.RunModeService
	c.Parse("nextdns "+cmd, args, useStorage)

	log, err := host.NewLogger("nextdns")
	if err != nil {
		log = host.NewConsoleLogger("nextdns")
		log.Warningf("Service logger error (switching to console): %v", err)
	}
	p := &proxySvc{
		log: log,
	}

	if c.SetupRouter {
		r := router.New()
		if err := r.Configure(&c); err != nil {
			log.Errorf("Configuring router: %v", err)
		}
		p.OnStarted = append(p.OnStarted, func() {
			log.Info("Setting up router")
			if err := r.Setup(); err != nil {
				log.Errorf("Setting up router: %v", err)
			}
		})
		p.OnStopped = append(p.OnStopped, func() {
			log.Info("Restore router settings")
			if err := r.Restore(); err != nil {
				log.Errorf("Restore router settings: %v", err)
			}
		})
	}

	if c.AutoActivate {
		p.OnStarted = append(p.OnStarted, func() {
			log.Info("Activating")
			if err := activate(c); err != nil {
				log.Errorf("Activate: %v", err)
			}
		})
		p.OnStopped = append(p.OnStopped, func() {
			log.Info("Deactivating")
			if err := deactivate(); err != nil {
				log.Errorf("Deactivate: %v", err)
			}
		})
	}

	startup := time.Now()
	p.resolver = &resolver.DNS{
		DOH: resolver.DOH{
			ExtraHeaders: http.Header{
				"User-Agent": []string{fmt.Sprintf("nextdns-cli/%s (%s; %s; %s)", version, platform, runtime.GOARCH, host.InitType())},
			},
		},
		Manager: nextdnsEndpointManager(log, c.HPM, func() bool {
			// Backward compat: the captive portal is now somewhat always enabled,
			// but for those who enabled it in the past, disable the delay after which
			// the fallback is disabled.
			if c.DetectCaptivePortals {
				return true
			}
			// Allow fallback to plain DNS for 10 minute after startup or after
			// a change of network configuration.
			return time.Since(startup) < 10*time.Minute
		}),
	}

	cacheSize, err := config.ParseBytes(c.CacheSize)
	if err != nil {
		return fmt.Errorf("%s: cannot parse cache size: %v", c.CacheSize, err)
	}
	if cacheSize > 0 {
		cache, err := lru.NewARC(int(cacheSize))
		if err != nil {
			log.Errorf("Cache init failed: %v", err)
		} else {
			maxTTL := uint32(c.CacheMaxTTL / time.Second)
			p.resolver.DNS53.Cache = cache
			p.resolver.DNS53.CacheMaxTTL = maxTTL
			p.resolver.DOH.Cache = cache
			p.resolver.DOH.CacheMaxTTL = maxTTL
		}
	}

	if len(c.Conf) == 0 || (len(c.Conf) == 1 && c.Conf.Get(nil, nil) != "") {
		// Optimize for no dynamic configuration.
		p.resolver.DOH.URL = "https://dns.nextdns.io/" + c.Conf.Get(nil, nil)
	} else {
		p.resolver.DOH.GetURL = func(q query.Query) string {
			return "https://dns.nextdns.io/" + c.Conf.Get(q.PeerIP, q.MAC)
		}
	}

	p.Proxy = proxy.Proxy{
		Addr:      c.Listen,
		Upstream:  p.resolver,
		BogusPriv: c.BogusPriv,
		UseHosts:  c.UseHosts,
		Timeout:   c.Timeout,
	}

	if len(c.Forwarders) > 0 {
		// Append default doh server at the end of the forwarder list as a catch all.
		fwd := make(config.Forwarders, 0, len(c.Forwarders)+1)
		fwd = append(fwd, c.Forwarders...)
		fwd = append(fwd, config.Resolver{Resolver: p.resolver})
		p.Upstream = &fwd
	}

	if c.LogQueries {
		p.QueryLog = func(q proxy.QueryInfo) {
			var errStr string
			if q.Error != nil {
				errStr = ": " + q.Error.Error()
			}
			dur := "cached"
			if !q.FromCache {
				dur = fmt.Sprintf("%dms", q.Duration/time.Millisecond)
			}
			log.Infof("Query %s %s %s %s (qry=%d/res=%d) %s %s%s",
				q.PeerIP.String(),
				q.Protocol,
				q.Type,
				q.Name,
				q.QuerySize,
				q.ResponseSize,
				dur,
				q.UpstreamTransport,
				errStr)
		}
	}
	p.InfoLog = func(msg string) {
		log.Info(msg)
	}
	p.ErrorLog = func(err error) {
		log.Error(err)
	}
	localhostMode := isLocalhostMode(&c)
	if c.ReportClientInfo {
		// Only enable discovery if configured to listen to requests outside
		// the local host.
		enableDiscovery := !localhostMode
		setupClientReporting(p, &c.Conf, enableDiscovery)
	}
	if localhostMode {
		// If only listening on localhost, we may be running on a laptop or
		// other sort of device that might change network from time to time.
		// When such change is detected, it better to trigger a re-negotiation
		// of the best endpoint sooner than later. We also reset the startup
		// time so plain DNS fallback happen again (useful for captive portals).
		p.OnInit = append(p.OnInit, func(ctx context.Context) {
			netChange := make(chan netstatus.Change)
			netstatus.Notify(netChange)
			for c := range netChange {
				log.Infof("Network change detected: %s", c)
				startup = time.Now() // reset the startup marker so DNS fallback can happen again.
				if err := p.resolver.Manager.Test(ctx); err != nil {
					log.Error("Test after network change failed: %v", err)
				}
			}
		})
	}

	if err = service.Run("nextdns", p); err != nil {
		log.Errorf("Startup failed: %v", err)
		return err
	}
	return nil
}

// isLocalhostMode returns true if listen is only listening for the local host.
func isLocalhostMode(c *config.Config) bool {
	if c.SetupRouter {
		// The listen arg is irrelevant when in router mode.
		return false
	}
	if host, _, err := net.SplitHostPort(c.Listen); err == nil {
		switch host {
		case "localhost", "127.0.0.1", "::1":
			return true
		}
		if ips := hosts.LookupHost(host); len(ips) > 0 {
			for _, ip := range ips {
				if !net.ParseIP(ip).IsLoopback() {
					return false
				}
			}
			return true
		}
		return net.ParseIP(host).IsLoopback()
	}
	return false
}

// nextdnsEndpointManager returns a endpoint.Manager configured to connect to
// NextDNS using different steering techniques.
func nextdnsEndpointManager(log host.Logger, hpm bool, canFallback func() bool) *endpoint.Manager {
	qs := "?stack=dual"
	if hpm {
		qs += "&hardened_privacy=1"
	}
	m := &endpoint.Manager{
		Providers: []endpoint.Provider{
			// Prefer unicast routing.
			&endpoint.SourceURLProvider{
				SourceURL: "https://router.nextdns.io" + qs,
				Client: &http.Client{
					Timeout: 5 * time.Second,
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
			log.Warningf("Endpoint failed: %v: %v", e, err)
		},
		OnProviderError: func(p endpoint.Provider, err error) {
			log.Warningf("Endpoint provider failed: %v: %v", p, err)
		},
		OnConnect: func(ci *endpoint.ConnectInfo) {
			log.Infof("Connected %s (con=%dms tls=%dms, %s)",
				ci.ServerAddr,
				ci.ConnectTimes[ci.ServerAddr]/time.Millisecond,
				ci.TLSTime/time.Millisecond,
				ci.TLSVersion)
		},
		OnChange: func(e endpoint.Endpoint) {
			log.Infof("Switching endpoint: %s", e)
		},
	}
	// Fallback on system DNS and set a short min test interval for when plain
	// DNS protocol is used so we go back on safe DoH as soon as possible. This
	// allows automatic handling of captive portals as well as NTP / DNS
	// inter-dependency on some routers, when NTP needs DNS to sync the time,
	// and DoH needs time properly set to establish a TLS session.
	m.Providers = append(m.Providers, endpoint.ProviderFunc(func(ctx context.Context) ([]endpoint.Endpoint, error) {
		if !canFallback() {
			// Fallback disabled.
			return nil, nil
		}
		ips := host.DNS()
		endpoints := make([]endpoint.Endpoint, 0, len(ips)+1)
		for _, ip := range ips {
			endpoints = append(endpoints, &endpoint.DNSEndpoint{
				Addr: net.JoinHostPort(ip, "53"),
			})
		}
		// Add NextDNS anycast IP in case none of the system DNS works or we did
		// not find any.
		endpoints = append(endpoints, &endpoint.DNSEndpoint{
			Addr: "45.90.28.0:53",
		})
		return endpoints, nil
	}))
	m.EndpointTester = func(e endpoint.Endpoint) endpoint.Tester {
		if e.Protocol() == endpoint.ProtocolDNS {
			// Return a tester than never fail so we are always selected as
			// a last resort when all other endpoints failed.
			return func(ctx context.Context, testDomain string) error {
				return nil
			}
		}
		return nil // default tester
	}
	m.GetMinTestInterval = func(e endpoint.Endpoint) time.Duration {
		if e.Protocol() == endpoint.ProtocolDNS {
			return 5 * time.Second
		}
		return 0 // use default MinTestInterval
	}
	return m
}

func setupClientReporting(p *proxySvc, conf *config.Configs, enableDiscovery bool) {
	deviceName, _ := host.Name()
	deviceID, _ := machineid.ProtectedID("NextDNS")
	if len(deviceID) > 5 {
		// No need to be globally unique.
		deviceID = deviceID[:5]
	}

	r := &discovery.Resolver{}
	if enableDiscovery {
		r.Register(&discovery.Hosts{})
		r.Register(&discovery.MDNS{})
		r.Register(&discovery.DHCP{})
		r.Register(&discovery.DNS{})
		p.OnInit = append(p.OnInit, func(ctx context.Context) {
			p.log.Info("Starting discovery resolver")
			ctx = discovery.WithTrace(ctx, discovery.Trace{
				OnDiscover: func(addr, host, source string) {
					p.log.Infof("Discovered(%s) %s = %s", source, addr, host)
				},
				OnWarning: func(msg string) {
					p.log.Warningf("Discovery: %s", msg)
				},
			})
			r.Start(ctx)
		})
	}

	p.resolver.DOH.ClientInfo = func(q query.Query) (ci resolver.ClientInfo) {
		if !q.PeerIP.IsLoopback() {
			// When acting as router, try to guess as much info as possible from
			// LAN client.
			ci.IP = q.PeerIP.String()
			ci.Name = r.Lookup(q.PeerIP.String())
			if q.MAC != nil {
				ci.ID = shortID(conf.Get(q.PeerIP, q.MAC), q.MAC)
				hex := q.MAC.String()
				if len(hex) >= 8 {
					// Only send the manufacturer part of the MAC.
					ci.Model = "mac:" + hex[:8]
				}
				if ci.Name == "" {
					ci.Name = r.Lookup(hex)
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
