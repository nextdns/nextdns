// +build linux freebsd openbsd netbsd dragonfly

package host

import (
	"net"
	"time"

	"github.com/nextdns/nextdns/internal/dnsmessage"
)

func guessDNS(strategy ...func() []string) (dns []string) {
	c := make(chan []string)
	for _, s := range strategy {
		go func(s func() []string) {
			c <- s()
		}(s)
	}
	for i := 0; i < len(strategy); i++ {
		dns = appendUniq(dns, <-c...)
	}
	return
}

func appendUniq(set []string, adds ...string) []string {
	for i := range adds {
		found := false
		for j := range set {
			if adds[i] == set[j] {
				found = true
				break
			}
		}
		if !found {
			set = append(set, adds[i])
		}
	}
	return set
}

func probeDNS(dns string) bool {
	c, err := net.Dial("udp", net.JoinHostPort(dns, "53"))
	if err != nil {
		return false
	}
	defer c.Close()
	if err = c.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return false
	}
	msg := dnsmessage.Message{
		Questions: []dnsmessage.Question{
			{
				Name:  dnsmessage.MustNewName("."),
				Type:  dnsmessage.TypeA,
				Class: dnsmessage.ClassINET,
			},
		},
	}
	buf, err := msg.Pack()
	if err != nil {
		return false
	}
	_, err = c.Write(buf)
	if err != nil {
		return false
	}
	_, err = c.Read(buf)
	return err == nil
}
