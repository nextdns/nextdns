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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/denisbrodbeck/machineid"

	"github.com/nextdns/nextdns/arp"
	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/ctl"
	"github.com/nextdns/nextdns/discovery"
	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/hosts"
	"github.com/nextdns/nextdns/ndp"
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
	p.log.Infof("Starting NextDNS %s/%s on %s", version, platform, strings.Join(p.Addrs, ", "))
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
	p.log.Infof("Restarting NextDNS %s/%s on %s", version, platform, strings.Join(p.Addrs, ", "))
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
	// When running interactive, ignore config file unless explicitly specified.
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

	ctl := ctl.Server{
		Addr: c.Control,
		OnConnect: func(c net.Conn) {
			log.Infof("Control client connected: %v", c)
		},
		OnDisconnect: func(c net.Conn) {
			log.Infof("Control client disconnected: %v", c)
		},
		OnEvent: func(c net.Conn, e ctl.Event) {
			log.Infof("Control client sent event: %v: %s(%v)", c, e.Name, e.Data)
		},
	}
	if err := ctl.Start(); err != nil {
		log.Errorf("Cannot start control server: %v", err)
	}
	defer func() { _ = ctl.Stop() }()
	ctl.Command("trace", func(data any) any {
		buf := make([]byte, 100*1024)
		n := runtime.Stack(buf, true)
		return string(buf[:n])
	})
	ctl.Command("ndp", func(data any) any {
		t, err := ndp.Get()
		if err != nil {
			return err.Error()
		}
		var sb strings.Builder
		for _, i := range t {
			fmt.Fprintf(&sb, "%s %s\n", i.IP, i.MAC)
		}
		return sb.String()
	})
	ctl.Command("arp", func(data any) any {
		t, err := arp.Get()
		if err != nil {
			return err.Error()
		}
		var sb strings.Builder
		for _, i := range t {
			fmt.Fprintf(&sb, "%s %s\n", i.IP, i.MAC)
		}
		return sb.String()
	})

	if c.SetupRouter {
		r := router.New()
		if err := r.Configure(&c); err != nil {
			log.Errorf("Configuring %s router: %v", r, err)
		}
		p.OnStarted = append(p.OnStarted, func() {
			log.Infof("Setting up %s router", r)
			if err := r.Setup(); err != nil {
				log.Errorf("Setting up %s router: %v", r, err)
			}
		})
		p.OnStopped = append(p.OnStopped, func() {
			log.Infof("Restore %s router settings", r)
			if err := r.Restore(); err != nil {
				log.Errorf("Restore %s router settings: %v", r, err)
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

	var startupUnix atomic.Int64
	startupUnix.Store(time.Now().UnixNano())
	p.resolver = &resolver.DNS{
		DOH: resolver.DOH{
			ExtraHeaders: http.Header{
				"User-Agent": []string{fmt.Sprintf("nextdns-cli/%s (%s; %s; %s)", version, platform, runtime.GOARCH, host.InitType())},
			},
		},
		Manager: nextdnsEndpointManager(log, c.Debug, func() bool {
			// Backward compat: the captive portal is now somewhat always enabled,
			// but for those who enabled it in the past, disable the delay after which
			// the fallback is disabled.
			if c.DetectCaptivePortals {
				return true
			}
			// Allow fallback to plain DNS for 10 minute after startup or after
			// a change of network configuration.
			startup := time.Unix(0, startupUnix.Load())
			return time.Since(startup) < 10*time.Minute
		}),
	}

	cacheSize, err := config.ParseBytes(c.CacheSize)
	if err != nil {
		return fmt.Errorf("%s: cannot parse cache size: %v", c.CacheSize, err)
	}
	var sharedCache resolver.Cacher
	var cacheMaxAge uint32
	if cacheSize > 0 {
		cc, err := resolver.NewByteCache(cacheSize, c.CacheMetrics)
		if err != nil {
			log.Errorf("Cache init failed: %v", err)
		} else {
			cacheMaxAge = uint32(c.CacheMaxAge / time.Second)
			sharedCache = cc
			p.resolver.DNS53.Cache = cc
			p.resolver.DNS53.CacheMaxAge = cacheMaxAge
			p.resolver.DOH.Cache = cc
			p.resolver.DOH.CacheMaxAge = cacheMaxAge
			if c.CacheMetrics {
				ctl.Command("cache-metrics", func(data any) any {
					m := cc.Metrics()
					if m == nil {
						return nil
					}
					return map[string]uint64{
						"hits":          m.Hits(),
						"misses":        m.Misses(),
						"ratio":         uint64(m.Ratio() * 1000), // per-mille for stable JSON type
						"keys_added":    m.KeysAdded(),
						"keys_updated":  m.KeysUpdated(),
						"keys_evicted":  m.KeysEvicted(),
						"cost_added":    m.CostAdded(),
						"cost_evicted":  m.CostEvicted(),
						"sets_dropped":  m.SetsDropped(),
						"sets_rejected": m.SetsRejected(),
						"gets_dropped":  m.GetsDropped(),
						"gets_kept":     m.GetsKept(),
					}
				})
			}
			ctl.Command("cache-stats", func(data any) any {
				return p.resolver.CacheStats()
			})
		}
	}
	maxTTL := uint32(c.MaxTTL / time.Second)
	p.resolver.DNS53.MaxTTL = maxTTL
	p.resolver.DOH.MaxTTL = maxTTL

	if len(c.Profile) == 0 || (len(c.Profile) == 1 && c.Profile.Get(nil, nil, nil) != "") {
		// Optimize for no dynamic configuration.
		profileID := c.Profile.Get(nil, nil, nil)
		profileURL := "https://dns.nextdns.io/" + profileID
		p.resolver.DOH.GetProfileURL = func(q query.Query) (string, string) {
			return profileURL, profileID
		}
	} else {
		p.resolver.DOH.GetProfileURL = func(q query.Query) (string, string) {
			profileID := c.Profile.Get(q.PeerIP, q.LocalIP, q.MAC)
			return "https://dns.nextdns.io/" + profileID, profileID
		}
	}

	p.Proxy = proxy.Proxy{
		Addrs:               c.Listens,
		Upstream:            p.resolver,
		BogusPriv:           c.BogusPriv,
		Timeout:             c.Timeout,
		MaxInflightRequests: c.MaxInflightRequests,
	}

	discoverHosts := &discovery.Hosts{OnError: func(err error) { log.Errorf("hosts: %v", err) }}
	if c.UseHosts {
		p.Proxy.LocalResolver = discovery.Resolver{discoverHosts}
	}
	localhostMode := isLocalhostMode(&c)
	if c.ReportClientInfo {
		// Only enable discovery if configured to listen to requests outside
		// the local host or if setup router is on.
		enableDiscovery := !localhostMode
		var r discovery.Resolver
		if enableDiscovery {
			discoverDHCP := &discovery.DHCP{OnError: func(err error) { log.Errorf("dhcp: %v", err) }}
			discoverDNS := &discovery.DNS{Upstream: c.DiscoveryDNS}
			var discoverMDNS discovery.Source = discovery.Dummy{}
			if c.MDNS != "disabled" {
				mdns := &discovery.MDNS{OnError: func(err error) { log.Errorf("mdns: %v", err) }}
				discoverMDNS = mdns
				p.OnInit = append(p.OnInit, func(ctx context.Context) {
					log.Info("Starting mDNS discovery")
					if err := mdns.Start(ctx, c.MDNS); err != nil {
						log.Errorf("Cannot start mDNS: %v", err)
					}
				})
			}
			discoveryResolver := discovery.Resolver{discoverMDNS, discoverDHCP}
			if c.DiscoveryDNS != "" {
				// Only include discovery DNS as discovery resolver if
				// explicitly specified as auto-discovered DNS discovery can
				// create loops.
				discoveryResolver = append(discovery.Resolver{discoverDNS}, discoveryResolver...)
			}
			p.Proxy.DiscoveryResolver = discoveryResolver
			r = discovery.Resolver{
				discoverHosts,
				&discovery.Merlin{},
				&discovery.Ubios{},
				&discovery.Firewalla{},
				discoverMDNS,
				discoverDHCP,
				discoverDNS,
			}
			ctl.Command("discovered", func(data any) any {
				d := map[string]map[string][]string{}
				r.Visit(func(source, name string, addrs []string) {
					if d[source] == nil {
						d[source] = map[string][]string{}
					}
					d[source][name] = addrs
				})
				return d
			})
		}
		setupClientReporting(p, &c.Profile, r)
	}
	if p.Proxy.DiscoveryResolver == nil && c.DiscoveryDNS != "" {
		p.Proxy.DiscoveryResolver = &discovery.DNS{Upstream: c.DiscoveryDNS}
	}

	if len(c.Forwarders) > 0 {
		// Append default doh server at the end of the forwarder list as a catch all.
		fwd := make(config.Forwarders, 0, len(c.Forwarders)+1)
		fwd = append(fwd, c.Forwarders...)
		fwd = append(fwd, config.Resolver{Resolver: p.resolver})
		if sharedCache != nil {
			for i := range fwd {
				if r, ok := fwd[i].Resolver.(*resolver.DNS); ok {
					r.DNS53.Cache = sharedCache
					r.DNS53.CacheMaxAge = cacheMaxAge
					r.DOH.Cache = sharedCache
					r.DOH.CacheMaxAge = cacheMaxAge
				}
			}
		}
		p.Upstream = &fwd
	}

	p.QueryLog = func(q proxy.QueryInfo) {
		if !c.LogQueries && q.Error == nil {
			return
		}
		var errStr string
		dur := "cached"
		if q.Error != nil {
			errStr = ": " + q.Error.Error()
			if q.FromCache {
				dur = "cache fallback"
			}
		}
		if !q.FromCache {
			dur = fmt.Sprintf("%dms", q.Duration/time.Millisecond)
		}
		profile := q.Profile
		if profile == "" {
			profile = "none"
		}
		log.Infof("Query %s %s %s %s %s (qry=%d/res=%d) %s %s%s",
			q.PeerIP.String(),
			q.Protocol,
			q.Type,
			q.Name,
			profile,
			q.QuerySize,
			q.ResponseSize,
			dur,
			q.UpstreamTransport,
			errStr)
	}
	p.InfoLog = func(msg string) {
		log.Info(msg)
	}
	p.ErrorLog = func(err error) {
		log.Error(err)
	}
	if localhostMode {
		// If only listening on localhost, we may be running on a laptop or
		// other sort of device that might change network from time to time.
		// When such change is detected, it better to trigger a re-negotiation
		// of the best endpoint sooner than later. We also reset the startup
		// time so plain DNS fallback happen again (useful for captive portals).
		p.OnInit = append(p.OnInit, func(ctx context.Context) {
			netChange := make(chan netstatus.Change, 1)
			netstatus.Notify(netChange)
			defer netstatus.Stop(netChange)
			for {
				select {
				case <-ctx.Done():
					return
				case c := <-netChange:
					log.Infof("Network change detected: %s", c)
					startupUnix.Store(time.Now().UnixNano()) // reset the startup marker so DNS fallback can happen again.
					if err := p.resolver.Manager.Test(ctx); err != nil && !errors.Is(err, context.Canceled) {
						log.Errorf("Test after network change failed: %v", err)
					}
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
	for _, listen := range c.Listens {
		if host, _, err := net.SplitHostPort(listen); err == nil {
			switch host {
			case "localhost", "127.0.0.1", "::1":
				continue
			}
			if ips := hosts.LookupHost(host); len(ips) > 0 {
				for _, ip := range ips {
					if !net.ParseIP(ip).IsLoopback() {
						return false
					}
				}
			} else if !net.ParseIP(host).IsLoopback() {
				return false
			}
		}
	}
	return true
}

// nextdnsEndpointManager returns a endpoint.Manager configured to connect to
// NextDNS using different steering techniques.
func nextdnsEndpointManager(log host.Logger, debug bool, canFallback func() bool) *endpoint.Manager {
	m := &endpoint.Manager{
		Providers: []endpoint.Provider{
			// Prefer unicast routing.
			&endpoint.SourceHTTPSSVCProvider{
				Hostname: "dns.nextdns.io",
				Source:   endpoint.MustNew("https://dns.nextdns.io#45.90.28.0,2a07:a8c0::,45.90.30.0,2a07:a8c1::"),
			},
			// Try routing without anycast bootstrap.
			// TOFIX: this creates circular dependency if the /etc/resolv.conf is setup to localhost.
			// &endpoint.SourceHTTPSSVCProvider{
			// 	Hostname: "dns.nextdns.io",
			// 	Source:   endpoint.MustNew("https://dns.nextdns.io"),
			// },
			// Fallback on anycast.
			endpoint.StaticProvider([]endpoint.Endpoint{
				endpoint.MustNew("https://dns1.nextdns.io#45.90.28.0,2a07:a8c0::"),
				endpoint.MustNew("https://dns2.nextdns.io#45.90.30.0,2a07:a8c1::"),
			}),
		},
		InitEndpoint: endpoint.MustNew("https://dns.nextdns.io#45.90.28.0,2a07:a8c0::,45.90.30.0,2a07:a8c1::"),
		OnError: func(e endpoint.Endpoint, err error) {
			log.Warningf("Endpoint failed: %v: %v", e, err)
		},
		OnProviderError: func(p endpoint.Provider, err error) {
			log.Warningf("Endpoint provider failed: %v: %v", p, err)
		},
		OnConnect: func(ci *endpoint.ConnectInfo) {
			log.Infof("Connected %s (con=%dms tls=%dms, %s, %s)",
				ci.ServerAddr,
				ci.ConnectTimes[ci.ServerAddr]/time.Millisecond,
				ci.TLSTime/time.Millisecond,
				ci.Protocol,
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
	if debug {
		m.DebugLog = func(msg string) {
			log.Debug(msg)
		}
	}
	return m
}

func setupClientReporting(p *proxySvc, conf *config.Profiles, r discovery.Resolver) {
	deviceName, _ := host.Name()
	deviceID, _ := machineid.ProtectedID("NextDNS")
	deviceModel := host.Model()
	if len(deviceID) > 5 {
		// No need to be globally unique.
		deviceID = deviceID[:5]
	}
	deviceID = strings.ToUpper(deviceID)

	p.resolver.DOH.ClientInfo = func(q query.Query) (ci resolver.ClientInfo) {
		if !q.PeerIP.IsLoopback() {
			// When acting as router, try to guess as much info as possible from
			// LAN client.
			ci.IP = q.PeerIP.String()
			ci.Name = normalizeName(r.LookupAddr(q.PeerIP.String()))
			if q.MAC != nil {
				ci.ID = shortID(conf.Get(q.PeerIP, q.LocalIP, q.MAC), q.MAC)
				hex := q.MAC.String()
				if len(hex) >= 8 {
					// Only send the manufacturer part of the MAC.
					ci.Model = "mac:" + hex[:8]
				}
				if names := r.LookupMAC(hex); len(names) > 0 {
					ci.Name = normalizeName(names)
				}
			}
			if ci.ID == "" {
				ci.ID = shortID(conf.Get(q.PeerIP, q.LocalIP, q.MAC), q.PeerIP)
			}
			return
		}

		ci.ID = deviceID
		ci.Name = deviceName
		ci.Model = deviceModel
		return
	}
}

func normalizeName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	name := names[0]
	if idx := strings.IndexByte(name, '.'); idx != -1 {
		name = name[:idx] // remove .local. suffix
	}
	return name
}

// shortID derives a non reversible 5 char long non globally unique ID from the
// the config + a device ID so device could not be tracked across configs.
func shortID(confID string, deviceID []byte) string {
	// Concat
	l := max(len(confID)+len(deviceID),
		// len(base32((1<<64)-1)) = 13
		13)
	buf := make([]byte, 0, l)
	buf = append(buf, confID...)
	buf = append(buf, deviceID...)
	// Hash
	sum := xxhash.Sum64(buf)
	// Base 32
	buf = strconv.AppendUint(buf[:0], sum, 32)
	// Trim 5
	if len(buf) < 5 {
		// Extremely unlikely, but keep it safe and deterministic.
		pad := make([]byte, 5)
		for i := 0; i < 5-len(buf); i++ {
			pad[i] = '0'
		}
		copy(pad[5-len(buf):], buf)
		buf = pad
	} else {
		buf = buf[:5]
	}
	// Uppercase
	for i := range buf {
		if buf[i] >= 'a' {
			buf[i] ^= 1 << 5
		}
	}
	return string(buf)
}
