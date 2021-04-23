module github.com/nextdns/nextdns

go 1.13

replace github.com/kardianos/service => github.com/rs/service v1.0.1-0.20191214021204-b1a37fd90075

require (
	github.com/Microsoft/go-winio v0.4.19
	github.com/cespare/xxhash v1.1.0
	github.com/denisbrodbeck/machineid v1.0.1
	github.com/hashicorp/golang-lru v0.5.4
	github.com/konsorten/go-windows-terminal-sequences v1.0.1 // indirect
	github.com/lucas-clemente/quic-go v0.20.1
	github.com/stretchr/objx v0.1.1 // indirect
	golang.org/x/crypto v0.0.0-20210421170649-83a5a9bb288b // indirect
	golang.org/x/net v0.0.0-20210421230115-4e50805a0758
	golang.org/x/sys v0.0.0-20210421221651-33663a62ff08
)
