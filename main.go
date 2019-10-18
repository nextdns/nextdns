package main

import (
	"context"
	"flag"
	"fmt"
	stdlog "log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/kardianos/service"

	"github.com/nextdns/nextdns/endpoint"
	"github.com/nextdns/nextdns/proxy"
)

var log service.Logger

type proxySvc struct {
	proxy.Proxy
	router *endpoint.Manager
	stop   func()
}

func (p *proxySvc) Start(s service.Service) error {
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
		if err := p.ListenAndServe(ctx); err != nil && err != context.Canceled {
			_ = log.Error(err)
		}
	}()
	return nil
}

func (p *proxySvc) Stop(s service.Service) error {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	return nil
}

func main() {
	listen := flag.String("listen", "localhost:53", "Listen address for UDP DNS proxy server.")
	config := flag.String("config", "", "NextDNS custom configuration id.")
	svcFlag := flag.String("service", "", fmt.Sprintf("Control the system service.\nValid actions: %s", strings.Join(service.ControlAction[:], ", ")))
	flag.Parse()

	svcConfig := &service.Config{
		Name:        "NextDNS",
		DisplayName: "NextDNS Proxy",
		Description: "NextDNS DNS53 to DoH proxy.",
	}
	p := &proxySvc{
		Proxy: proxy.Proxy{
			Addr:     *listen,
			Upstream: "https://dns.nextdns.io/" + *config,
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
	p.QueryLog = func(q proxy.QueryInfo) {
		_ = log.Infof("%s %s %d/%d %d", q.Protocol, q.Name, q.QuerySize, q.ResponseSize, q.Duration/time.Millisecond)
	}
	p.ErrorLog = func(err error) {
		_ = log.Error(err)
	}
	if len(*svcFlag) != 0 {
		err := service.Control(s, *svcFlag)
		if err != nil {
			stdlog.Fatal(err)
		}
		return
	}
	if err = s.Run(); err != nil {
		_ = log.Error(err)
	}
}
