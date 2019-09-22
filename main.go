package main

import (
	"context"
	"flag"
	"fmt"
	stdlog "log"
	"strings"
	"time"

	"github.com/nextdns/nextdns/proxy"

	"github.com/kardianos/service"
)

var log service.Logger

type proxySvc struct {
	proxy.Proxy
	stop func()
}

func (p *proxySvc) Start(s service.Service) error {
	go func() {
		var ctx context.Context
		ctx, p.stop = context.WithCancel(context.Background())
		defer p.stop()
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
