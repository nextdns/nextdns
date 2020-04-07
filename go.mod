module github.com/nextdns/nextdns

go 1.13

replace github.com/kardianos/service => github.com/rs/service v1.0.1-0.20191214021204-b1a37fd90075

require (
	github.com/cespare/xxhash v1.1.0
	github.com/denisbrodbeck/machineid v1.0.1
	github.com/hashicorp/golang-lru v0.5.4
	golang.org/x/net v0.0.0-20191209160850-c0dbc17a3553
	golang.org/x/sys v0.0.0-20191115151921-52ab43148777
)
