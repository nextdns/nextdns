package query

import (
	"fmt"
	"net"
	"strconv"

	"github.com/nextdns/nextdns/arp"
	"github.com/nextdns/nextdns/ndp"
	"golang.org/x/net/dns/dnsmessage"
)

type Query struct {
	ID               uint16
	Class            Class
	Type             Type
	RecursionDesired bool
	MsgSize          uint16
	Name             string
	LocalIP          net.IP
	PeerIP           net.IP
	MAC              net.HardwareAddr
	Payload          []byte
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
func New(payload []byte, peerIP, localIP net.IP) (Query, error) {
	q := Query{
		LocalIP: localIP,
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
	qry.RecursionDesired = h.RecursionDesired
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
			// The parser copies option data but not its offset, so track each
			// option's offset in lockstep with the parser's own walk from the
			// start of the OPT rdata. That yields the exact position without a
			// search, so it cannot be diverted onto lookalike bytes elsewhere,
			// nor defeated by an option that overruns a malformed rdlen.
			off := optRDataStart(qry.Payload)
			for _, o := range opt.Options {
				switch o.Code {
				case EDNS0_MAC:
					qry.MAC = net.HardwareAddr(o.Data)
				case EDNS0_SUBNET:
					if len(o.Data) >= 8 {
						switch o.Data[1] {
						case 0x1: // IPv4
							if o.Data[2] == 32 {
								// Only consider full IPs
								qry.PeerIP = net.IP(o.Data[4:8])
							}
							// Avoid leaking ECS to the upstream.
							if off >= 0 {
								nutterECSOption(qry.Payload, off, o)
							}
						case 0x2: // IPv6
							if o.Data[2] == 128 && len(o.Data) >= 20 {
								// Only consider full IPs
								qry.PeerIP = net.IP(o.Data[4:20])
							}
							// Avoid leaking ECS to the upstream.
							if off >= 0 {
								nutterECSOption(qry.Payload, off, o)
							}
						}
					}
				}
				if off >= 0 {
					off += 4 + len(o.Data) // advance to the next option, as the parser did
				}
			}
			break
		}
		// Consume the non-OPT record. Without this, the next AdditionalHeader
		// call re-reads this same unconsumed header and the loop never ends --
		// an infinite loop reachable from a single query.
		if err := p.SkipAdditional(); err != nil {
			return fmt.Errorf("parse additional: %v", err)
		}
	}

	return nil
}

func nutterECSOption(payload []byte, off int, o dnsmessage.Option) {
	end := off + 4 + len(o.Data)
	if off < 0 || end > len(payload) {
		return
	}
	// Only proceed if the option code at off matches the one the parser handed
	// us, so a drifted offset can never zero the wrong bytes.
	if int(payload[off])<<8|int(payload[off+1]) != int(o.Code) {
		return
	}
	// Zero all bits of the ECS option data.
	for i := off + 4; i < end; i++ {
		payload[i] = 0
	}
	// Set the ECS option to an invalid code so the upstream does not treat the
	// zeroed /0 as a request to suppress its own ECS.
	payload[off] = 0xFF
	payload[off+1] = 0xFF
}

// optRDataStart returns the offset where the first OPT record's rdata (its EDNS
// option list) begins, walking the wire format directly since the parser does
// not expose offsets. It returns -1 if there is no OPT record or the message is
// malformed; every step is bounds-checked against untrusted input.
func optRDataStart(msg []byte) int {
	if len(msg) < 12 {
		return -1
	}
	counts := []int{
		int(msg[4])<<8 | int(msg[5]),   // questions
		int(msg[6])<<8 | int(msg[7]),   // answers
		int(msg[8])<<8 | int(msg[9]),   // authorities
		int(msg[10])<<8 | int(msg[11]), // additionals
	}
	off := 12
	skipName := func() bool {
		for {
			if off >= len(msg) {
				return false
			}
			c := int(msg[off])
			switch {
			case c&0xC0 == 0xC0: // compression pointer ends the name
				off += 2
				return off <= len(msg)
			case c == 0:
				off++
				return true
			default:
				off += 1 + c
			}
		}
	}
	for i := 0; i < counts[0]; i++ { // questions: name + type + class
		if !skipName() || off+4 > len(msg) {
			return -1
		}
		off += 4
	}
	rr := func() (typ, rdlen int, ok bool) {
		if !skipName() || off+10 > len(msg) {
			return 0, 0, false
		}
		typ = int(msg[off])<<8 | int(msg[off+1])
		rdlen = int(msg[off+8])<<8 | int(msg[off+9])
		off += 10
		if off+rdlen > len(msg) {
			return 0, 0, false
		}
		return typ, rdlen, true
	}
	for i := 0; i < counts[1]+counts[2]; i++ { // answers + authorities
		_, rdlen, ok := rr()
		if !ok {
			return -1
		}
		off += rdlen
	}
	for i := 0; i < counts[3]; i++ { // additionals
		typ, rdlen, ok := rr()
		if !ok {
			return -1
		}
		if typ == int(dnsmessage.TypeOPT) {
			return off
		}
		off += rdlen
	}
	return -1
}
