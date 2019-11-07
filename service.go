package main

import (
	"context"
	"flag"
	"fmt"
	stdlog "log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/denisbrodbeck/machineid"
	"github.com/kardianos/service"

	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/endpoint"
	"github.com/nextdns/nextdns/mdns"
	"github.com/nextdns/nextdns/oui"
	"github.com/nextdns/nextdns/proxy"
)

const OUIURL = "http://standards.ieee.org/develop/regauth/oui/oui.txt"

var log service.Logger

type proxySvc struct {
	proxy.Proxy
	router *endpoint.Manager
	init   []func(ctx context.Context)
	stop   func()
}

func (p *proxySvc) Start(s service.Service) (err error) {
	errC := make(chan error)
	go func() {
		var ctx context.Context
		ctx, p.stop = context.WithCancel(context.Background())
		defer p.stop()
		if p.router != nil {
			if err := p.router.Test(ctx); err != nil && err != context.Canceled {
				_ = log.Error(err)
				return
			}
		}
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
	conf := &config.Rules{}
	logQueries := new(bool)
	reportClientInfo := new(bool)
	if cmd == "run" || cmd == "install" {
		listen = flag.String("listen", "localhost:53", "Listen address for UDP DNS proxy server.")
		conf = config.Flag("config", "NextDNS custom configuration id.\n"+
			"\n"+
			"The configuration id can be prefixed with a condition that is match for each query:\n"+
			"* 10.0.3.0/24=abcdef: A CIDR can be used to restrict a configuration to a subnet.\n"+
			"* 00:1c:42:2e:60:4a=abcdef: A MAC address can be used to restrict configuration\n"+
			" to a specific host on the LAN.\n"+
			"\n"+
			"This parameter can be repeated. The first match wins.")
		logQueries = flag.Bool("log-queries", false, "Log DNS query.")
		reportClientInfo = flag.Bool("report-client-info", false, "Embed clients information with queries.")
	}
	flag.Parse()

	svcConfig := &service.Config{
		Name:        "nextdns",
		DisplayName: "NextDNS Proxy",
		Description: "NextDNS DNS53 to DoH proxy.",
		Arguments:   append([]string{"run"}, os.Args[1:]...),
	}
	p := &proxySvc{
		Proxy: proxy.Proxy{
			Addr: *listen,
			Upstream: func(q proxy.Query) string {
				return "https://dns.nextdns.io/" + conf.Get(q.PeerIP, q.MAC)
			},
			ExtraHeaders: http.Header{
				"User-Agent": []string{fmt.Sprintf("nextdns-unix/%s (%s; %s)", version, platform, runtime.GOARCH)},
			},
		},
	}

	p.router = &endpoint.Manager{
		Providers: []endpoint.Provider{
			// Prefer unicast routing.
			endpoint.SourceURLProvider{
				SourceURL: "https://router.nextdns.io",
				Client: &http.Client{
					// Trick to avoid depending on DNS to contact the router API.
					Transport: endpoint.NewTransport(
						endpoint.New("router.nextdns.io", "", []string{
							"216.239.32.21",
							"216.239.34.21",
							"216.239.36.21",
							"216.239.38.21",
						}[rand.Intn(3)])),
				},
			},
			// Fallback on anycast.
			endpoint.StaticProvider(endpoint.New("dns1.nextdns.io", "", "45.90.28.0")),
			endpoint.StaticProvider(endpoint.New("dns2.nextdns.io", "", "45.90.30.0")),
		},
		OnError: func(e endpoint.Endpoint, err error) {
			_ = log.Warningf("Endpoint failed: %s: %v", e.Hostname, err)
		},
		OnChange: func(e endpoint.Endpoint, rt http.RoundTripper) {
			_ = log.Infof("Switching endpoint: %s", e.Hostname)
			p.Transport = rt
		},
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
			_ = log.Infof("%s (%s/%s/%s) %s %s (%d/%d) %d",
				q.Query.PeerIP.String(),
				q.ClientInfo.ID,
				q.ClientInfo.Model,
				q.ClientInfo.Name,
				q.Query.Protocol,
				q.Query.Name,
				len(q.Query.Payload),
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
			setupClientReporting(p)
		}
		return s.Run()
	default:
		panic("unknown cmd: " + cmd)
	}
}

func setupClientReporting(p *proxySvc) {
	deviceName, _ := os.Hostname()
	deviceID, _ := machineid.ProtectedID("NextDNS")
	if len(deviceID) > 5 {
		// No need to be globally unique.
		deviceID = deviceID[:5]
	}

	var ouiDb oui.OUI
	p.init = append(p.init, func(ctx context.Context) {
		var err error
		backoff := 1 * time.Second
		for {
			_ = log.Info("Loading OUI database")
			ouiDb, err = oui.Load(ctx, OUIURL)
			if err != nil {
				_ = log.Warningf("Cannot load OUI database: %v", err)
				// Retry.
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return
				}
				if backoff < time.Minute {
					backoff <<= 1
				}
				continue
			}
			break
		}
	})

	mdns := &mdns.Resolver{}
	p.init = append(p.init, func(ctx context.Context) {
		_ = log.Info("Starting mDNS resolver")
		if err := mdns.Start(ctx); err != nil {
			_ = log.Warningf("Cannot start mDNS resolver: %v", err)
		}
	})

	p.ClientInfo = func(q proxy.Query) (ci proxy.ClientInfo) {
		if !q.PeerIP.IsLoopback() {
			ci.ID = q.PeerIP.String()
			ci.Name = mdns.Lookup(q.PeerIP)
			if ci.Name == "" {
				ci.Name = ci.ID
			}
			if q.MAC != nil {
				ci.ID = shortMAC(q.MAC)
				ci.Model = ouiDb.Lookup(q.MAC)
			}
		} else {
			ci.ID = deviceID
			ci.Name = deviceName
		}
		return
	}
}

// shortMAC takes only the last 2 bytes to make per config unique ID.
func shortMAC(mac net.HardwareAddr) string {
	return fmt.Sprintf("%02x%02x", mac[len(mac)-2], mac[len(mac)-1])
}
