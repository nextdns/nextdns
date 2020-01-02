package resolver

import (
	"fmt"
	"net"

	"github.com/nextdns/nextdns/arp"
	"github.com/nextdns/nextdns/internal/dnsmessage"
)

type Query struct {
	Type    string
	Name    string
	PeerIP  net.IP
	MAC     net.HardwareAddr
	Payload []byte
}

var typeNames = map[dnsmessage.Type]string{
	dnsmessage.TypeA:     "A",
	dnsmessage.TypeNS:    "NS",
	dnsmessage.TypeCNAME: "CNAME",
	dnsmessage.TypeSOA:   "SOA",
	dnsmessage.TypePTR:   "PTR",
	dnsmessage.TypeMX:    "MX",
	dnsmessage.TypeTXT:   "TXT",
	dnsmessage.TypeAAAA:  "AAAA",
	dnsmessage.TypeSRV:   "SRV",
	dnsmessage.TypeOPT:   "OPT",
	dnsmessage.TypeWKS:   "WKS",
	dnsmessage.TypeHINFO: "HINFO",
	dnsmessage.TypeMINFO: "MINFO",
	dnsmessage.TypeAXFR:  "AXFR",
	dnsmessage.TypeALL:   "ALL",
}

// NewQuery lasily parses payload and extract the queried name, ip/MAC if
// present in the query as EDNS0 extension. ARP queries are performed to find
// MAC or IP depending on which one is present or not in the query.
func NewQuery(payload []byte, peerIP net.IP) (Query, error) {
	q := Query{
		PeerIP:  peerIP,
		Payload: payload,
	}

	if !peerIP.IsLoopback() {
		q.MAC = arp.SearchMAC(peerIP)
	}

	if err := q.parse(); err != nil {
		return q, err
	}

	if peerIP.IsLoopback() && q.MAC != nil {
		// MAC was sent in the request with a localhost client, it means we have
		// a proxy like dnsmasq in front of us, not able to send the client IP
		// using ECS. Let's search the IP in the arp table.
		if ip := arp.SearchIP(q.MAC); ip != nil {
			q.PeerIP = ip
		}
	}

	return q, nil
}

func (qry *Query) parse() error {
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
	qry.Type = typeNames[q.Type]
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
