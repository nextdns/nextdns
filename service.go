package main

import (
	"context"
	"errors"
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

	"github.com/nextdns/nextdns/config"
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
	stopFunc func()
	stopped  chan struct{}

	OnStarted func()
	OnStopped func()
}

func (p *proxySvc) Start(s service.Service) (err error) {
	_ = log.Infof("Starting NextDNS %s/%s on %s", version, platform, p.Addr)
	if err = p.start(); err != nil {
		return err
	}
	if p.OnStarted != nil {
		p.OnStarted()
	}
	return nil
}

func (p *proxySvc) start() (err error) {
	errC := make(chan error)
	go func() {
		var ctx context.Context
		ctx, p.stopFunc = context.WithCancel(context.Background())
		defer p.stopFunc()
		p.stopped = make(chan struct{})
		defer close(p.stopped)
		for _, f := range p.init {
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
		_ = log.Errorf("Start error: %v", err)
		return err
	case <-time.After(5 * time.Second):
	}
	return nil
}

func (p *proxySvc) Restart() error {
	_ = log.Infof("Restarting NextDNS %s/%s on %s", version, platform, p.Addr)
	_ = p.stop()
	return p.start()
}

func (p *proxySvc) Stop(s service.Service) error {
	_ = log.Infof("Stopping NextDNS on %s", p.Addr)
	if p.stop() {
		if p.OnStopped != nil {
			p.OnStopped()
		}
	}
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

func svc(cmd string) error {
	var c config.Config
	if cmd == "run" || cmd == "install" || cmd == "config" {
		c.Parse(os.Args[1:])
	}

	svcConfig := &service.Config{
		Name:        "nextdns",
		DisplayName: "NextDNS Proxy",
		Description: "NextDNS DNS53 to DoH proxy.",
		Arguments:   []string{"run", "-config-file", c.File},
		Dependencies: []string{
			"After=network-online.target",
			"Wants=network-online.target",
			"Before=nss-lookup.target",
			"Wants=nss-lookup.target",
		},
	}

	p := &proxySvc{}

	if c.AutoActivate {
		p.OnStarted = func() {
			_ = log.Info("Activating")
			if err := activate(); err != nil {
				_ = log.Errorf("Activate: %v", err)
			}
		}
		p.OnStopped = func() {
			_ = log.Info("Deactivating")
			if err := deactivate(); err != nil {
				_ = log.Errorf("Deactivate: %v", err)
			}
		}
	}

	p.resolver = &resolver.DNS{
		DOH: resolver.DOH{
			ExtraHeaders: http.Header{
				"User-Agent": []string{fmt.Sprintf("nextdns-cli/%s (%s; %s)", version, platform, runtime.GOARCH)},
			},
		},
		Manager: nextdnsEndpointManager(c.HPM, c.DetectCaptivePortals),
	}

	if len(c.Conf) == 0 || (len(c.Conf) == 1 && c.Conf.Get(nil, nil) != "") {
		// Optimize for no dynamic configuration.
		p.resolver.DOH.URL = "https://dns.nextdns.io/" + c.Conf.Get(nil, nil)
	} else {
		p.resolver.DOH.GetURL = func(q resolver.Query) string {
			return "https://dns.nextdns.io/" + c.Conf.Get(q.PeerIP, q.MAC)
		}
	}

	p.Proxy = proxy.Proxy{
		Addr:      c.Listen,
		Upstream:  p.resolver,
		BogusPriv: c.BogusPriv,
		Timeout:   c.Timeout,
	}

	if len(c.Forwarders) > 0 {
		// Append default doh server at the end of the forwarder list as a catch all.
		fwd := make(config.Forwarders, 0, len(c.Forwarders)+1)
		fwd = append(fwd, c.Forwarders...)
		fwd = append(fwd, config.Resolver{Resolver: p.resolver})
		p.Upstream = &fwd
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
	if c.LogQueries {
		p.QueryLog = func(q proxy.QueryInfo) {
			_ = log.Infof("Query %s %s %s %s (qry=%d/res=%d) %dms %s",
				q.PeerIP.String(),
				q.Protocol,
				q.Type,
				q.Name,
				q.QuerySize,
				q.ResponseSize,
				q.Duration/time.Millisecond,
				q.UpstreamTransport)
		}
	}
	p.ErrorLog = func(err error) {
		_ = log.Error(err)
	}
	switch cmd {
	case "install":
		_ = service.Control(s, "stop")
		_ = service.Control(s, "uninstall")
		if err := c.Save(); err != nil {
			fmt.Printf("Cannot write config: %v", err)
			os.Exit(1)
		}
		err := service.Control(s, "install")
		if err == nil {
			err = service.Control(s, "start")
		}
		return err
	case "uninstall":
		_ = deactivate()
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
		return nil
	case "run":
		if c.ReportClientInfo {
			setupClientReporting(p, &c.Conf)
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
	case "config":
		return c.Write(os.Stdout)
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

func setupClientReporting(p *proxySvc, conf *config.Configs) {
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
