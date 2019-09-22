package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/nextdns/nextdns/proxy"
)

func main() {
	listen := flag.String("listen", "localhost:53", "Listen address for UDP DNS proxy server.")
	config := flag.String("config", "", "NextDNS custom configuration id.")
	flag.Parse()

	log := log.New(os.Stderr, "", 0)
	p := proxy.Proxy{
		Addr:     *listen,
		Upstream: "https://dns.nextdns.io/" + *config,
		Client:   http.DefaultClient,
		QueryLog: func(proto, qname string, qsize, rsize int) {
			log.Printf("%s %s %d/%d", proto, qname, qsize, rsize)
		},
		ErrorLog: func(err error) {
			log.Print(err)
		},
	}
	if err := p.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
