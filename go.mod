module github.com/nextdns/nextdns

go 1.13

replace github.com/kardianos/service => github.com/rs/service v1.0.1-0.20191214021204-b1a37fd90075

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cespare/xxhash v1.1.0
	github.com/denisbrodbeck/machineid v1.0.1
	github.com/grandcat/zeroconf v0.0.0-20190424104450-85eadb44205c
	github.com/miekg/dns v1.1.22 // indirect
	github.com/pkg/errors v0.8.1 // indirect
	golang.org/x/net v0.0.0-20191105084925-a882066a44e0
	golang.org/x/sys v0.0.0-20191115151921-52ab43148777
)
