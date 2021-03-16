package query

import (
	"fmt"
	"net"
	"strconv"

	"github.com/nextdns/nextdns/arp"
	"github.com/nextdns/nextdns/internal/dnsmessage"
	"github.com/nextdns/nextdns/ndp"
)

type Query struct {
	ID      uint16
	Class   Class
	Type    Type
	MsgSize uint16
	Name    string
	PeerIP  net.IP
	MAC     net.HardwareAddr
	Payload []byte
}

type Class uint16

const (
	// ResourceHeader.Class and Question.Class
	ClassINET   Class = 1
	ClassCSNET  Class = 2
	ClassCHAOS  Class = 3
	ClassHESIOD Class = 4

	// Question.Class
	ClassANY Class = 255
)

var classNames = map[Class]string{
	ClassINET:   "INET",
	ClassCSNET:  "CSNET",
	ClassCHAOS:  "CHAOS",
	ClassHESIOD: "HESIOD",
	ClassANY:    "ANY",
}

func (c Class) String() string {
	s, found := classNames[c]
	if !found {
		s = strconv.FormatInt(int64(c), 10)
	}
	return s
}

type Type uint16

const (
	// ResourceHeader.Type and Question.Type
	TypeA     Type = 1
	TypeNS    Type = 2
	TypeCNAME Type = 5
	TypeSOA   Type = 6
	TypePTR   Type = 12
	TypeMX    Type = 15
	TypeTXT   Type = 16
	TypeAAAA  Type = 28
	TypeSRV   Type = 33
	TypeOPT   Type = 41

	// Question.Type
	TypeWKS   Type = 11
	TypeHINFO Type = 13
	TypeMINFO Type = 14
	TypeAXFR  Type = 252
	TypeALL   Type = 255
)

var typeNames = map[Type]string{
	TypeA:     "A",
	TypeNS:    "NS",
	TypeCNAME: "CNAME",
	TypeSOA:   "SOA",
	TypePTR:   "PTR",
	TypeMX:    "MX",
	TypeTXT:   "TXT",
	TypeAAAA:  "AAAA",
	TypeSRV:   "SRV",
	TypeOPT:   "OPT",
	TypeWKS:   "WKS",
	TypeHINFO: "HINFO",
	TypeMINFO: "MINFO",
	TypeAXFR:  "AXFR",
	TypeALL:   "ALL",
}

func (t Type) String() string {
	s, found := typeNames[t]
	if !found {
		s = strconv.FormatInt(int64(t), 10)
	}
	return s
}

const (
	EDNS0_SUBNET = 0x8
	EDNS0_MAC    = 0xfde9 // as defined by dnsmasq --add-mac feature
)

const maxDNSSize = 512

// New lasily parses payload and extract the queried name, ip/MAC if
// present in the query as EDNS0 extension. ARP queries are performed to find
// MAC or IP depending on which one is present or not in the query.
func New(payload []byte, peerIP net.IP) (Query, error) {
	q := Query{
		PeerIP:  peerIP,
		MsgSize: maxDNSSize,
		Payload: payload,
	}

	if !peerIP.IsLoopback() {
		if peerIP.To4() != nil {
			q.MAC = arp.SearchMAC(peerIP)
		} else {
			q.MAC = ndp.SearchMAC(peerIP)
		}

	}

	if err := q.parse(); err != nil {
		return q, err
	}

	if q.PeerIP.IsLoopback() && q.MAC != nil {
		// MAC was sent in the request with a localhost client, it means we have
		// a proxy like dnsmasq in front of us, not able to send the client IP
		// using ECS. Let's search the IP in the arp and/or ndp tables.
		if ip := arp.SearchIP(q.MAC); ip != nil {
			q.PeerIP = ip
		} else if ip := ndp.SearchIP(q.MAC); ip != nil {
			q.PeerIP = ip
		}
	}

	return q, nil
}

func (qry *Query) parse() error {
	p := &dnsmessage.Parser{}
	h, err := p.Start(qry.Payload)
	if err != nil {
		return fmt.Errorf("parse query: %v", err)
	}

	q, err := p.Question()
	if err != nil {
		return fmt.Errorf("parse question: %v", err)
	}
	qry.ID = h.ID
	qry.Class = Class(q.Class)
	qry.Type = Type(q.Type)
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
			qry.MsgSize = uint16(h.Class)
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
