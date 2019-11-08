package dns53

import (
	"net"

	"github.com/nextdns/nextdns/proxy"
)

type Resolver struct {
	Addr *net.UDPAddr
}

func (r Resolver) Resolve(q proxy.Query, buf []byte) (int, error) {
	c, err := net.DialUDP("udp", nil, r.Addr)
	if err != nil {
		return -1, err
	}
	defer c.Close()
	_, err = c.Write(q.Payload)
	if err != nil {
		return -1, err
	}
	return c.Read(buf)
}
