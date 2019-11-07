package proxy

import (
	"fmt"
	"net"

	"golang.org/x/net/dns/dnsmessage"
)

type Query struct {
	Protocol string
	Name     string
	PeerIP   net.IP
	MAC      net.HardwareAddr
	Payload  []byte
}

func (qry *Query) Parse() error {
	const (
		EDNS0_SUBNET = 0x8
		EDNS0_MAC    = 0xfde9 // as defined by dnsmasq --add-mac feature
	)

	p := &dnsmessage.Parser{}
	if _, err := p.Start(qry.Payload); err != nil {
		return fmt.Errorf("parse query: %v", err)
	}

	q, err := p.Question()
	if err != nil {
		return fmt.Errorf("parse question: %v", err)
	}
	qry.Name = q.Name.String()
	_ = p.SkipAllQuestions()
	_ = p.SkipAllAnswers()
	_ = p.SkipAllAuthorities()
	for {
		h, err := p.AdditionalHeader()
		if err != nil {
			if err == dnsmessage.ErrSectionDone {
				break
			}
			return fmt.Errorf("parse additional: %v", err)
		}
		if h.Type == dnsmessage.TypeOPT {
			opt, err := p.OPTResource()
			if err != nil {
				return fmt.Errorf("parse OPT: %v", err)
			}
			for _, o := range opt.Options {
				switch o.Code {
				case EDNS0_MAC:
					qry.MAC = net.HardwareAddr(o.Data)
				case EDNS0_SUBNET:
					if len(o.Data) < 8 {
						continue
					}
					switch o.Data[1] {
					case 0x1: // IPv4
						if o.Data[2] != 32 {
							// Only consider full IPs
							continue
						}
						qry.PeerIP = net.IP(o.Data[4:8])
					case 0x2: // IPv6
						if len(o.Data) < 20 {
							continue
						}
						if o.Data[2] != 128 {
							// Only consider full IPs
							continue
						}
						qry.PeerIP = net.IP(o.Data[4:20])
					}
				}
			}
			break
		}
	}

	return nil
}
