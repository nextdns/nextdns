package proxy

import (
	"errors"
	"strings"

	"github.com/nextdns/nextdns/resolver/query"
	"golang.org/x/net/dns/dnsmessage"
)

// truncateResponse emits a complete DNS message no larger than limit. A
// malformed upstream message becomes SERVFAIL rather than a wire cut.
func truncateResponse(buf []byte, n, limit int, q query.Query) int {
	if n <= limit {
		return n
	}
	if m := rebuildTruncated(buf, n, limit, q); m > 0 {
		return m
	}
	return errorResponse(buf, q, dnsmessage.RCodeServerFailure, limit)
}

func rebuildTruncated(buf []byte, n, limit int, q query.Query) int {
	var msg dnsmessage.Message
	if err := msg.Unpack(buf[:n]); err != nil {
		return 0
	}
	qEnd, authorityEnd, err := dnsSectionEnds(buf[:n])
	if err != nil || qEnd > limit {
		return 0
	}
	var opt *dnsmessage.OPTResource
	var optHdr dnsmessage.ResourceHeader
	for _, rr := range msg.Additionals {
		switch rr.Header.Type {
		case dnsmessage.TypeOPT:
			if opt != nil || rr.Header.Name.String() != "." {
				return 0
			}
			optHdr, opt = rr.Header, rr.Body.(*dnsmessage.OPTResource)
		case 250: // TSIG: changing the message requires the signing key.
			return 0
		case 24: // SIG(0), as opposed to a legacy SIG covering an RRset.
			if r, ok := rr.Body.(*dnsmessage.UnknownResource); ok && len(r.Data) >= 2 && r.Data[0] == 0 && r.Data[1] == 0 {
				return 0
			}
		}
	}
	edns, do := clientEDNS(q)
	if !edns {
		opt = nil // RFC 6891 forbids OPT without OPT in the client request.
	} else if opt == nil {
		_ = optHdr.SetEDNS0(limit, msg.RCode, do)
		opt = &dnsmessage.OPTResource{}
	}
	if opt != nil {
		optHdr.Class = dnsmessage.Class(limit)
	}

	// Keep the original answer/authority bytes. Repacking unknown RR bodies
	// could invalidate compression pointers in legacy record types.
	if authorityEnd <= limit {
		base := append([]byte(nil), buf[:authorityEnd]...)
		base[10], base[11] = 0, 0
		glue, err := requiredGlue(msg)
		if err != nil {
			return 0
		}
		if len(glue) > 12 {
			base = append(base, glue[12:]...)
			base[10], base[11] = glue[10], glue[11]
		}
		if opt == nil && len(base) <= limit {
			return copyValidMessage(buf, base)
		}
		if opt != nil {
			if m, dropped, ok := appendOPT(base, optHdr, *opt, limit); ok {
				if dropped {
					m[2] |= 0x2
				}
				return copyValidMessage(buf, m)
			}
		}
	}

	// A required RRset, referral glue, or required EDNS option cannot fit.
	// Return the question and OPT with TC, without splitting any records.
	base := append([]byte(nil), buf[:qEnd]...)
	base[2] |= 0x2
	clear(base[6:12])
	if opt == nil {
		return copyValidMessage(buf, base)
	}
	if m, _, ok := appendOPT(base, optHdr, *opt, limit); ok {
		return copyValidMessage(buf, m)
	}
	return 0
}

func copyValidMessage(dst, msg []byte) int {
	var parsed dnsmessage.Message
	if err := parsed.Unpack(msg); err != nil {
		return 0
	}
	return copy(dst, msg)
}

func errorResponse(buf []byte, q query.Query, rcode dnsmessage.RCode, limit int) int {
	name, err := dnsmessage.NewName(q.Name)
	if err != nil {
		return 0
	}
	h := dnsmessage.Header{ID: q.ID, Response: true, RecursionDesired: q.RecursionDesired, RCode: rcode}
	b := dnsmessage.NewBuilder(buf[:0], h)
	if err = b.StartQuestions(); err != nil || b.Question(dnsmessage.Question{Name: name, Type: dnsmessage.Type(q.Type), Class: dnsmessage.Class(q.Class)}) != nil {
		return 0
	}
	msg, err := b.Finish()
	if err != nil || len(msg) > limit {
		return 0
	}
	copy(buf, msg)
	if edns, do := clientEDNS(q); edns {
		var oh dnsmessage.ResourceHeader
		_ = oh.SetEDNS0(limit, rcode, do)
		if m, _, ok := appendOPT(buf[:len(msg)], oh, dnsmessage.OPTResource{}, limit); ok {
			copy(buf, m)
			return len(m)
		}
	}
	return len(msg)
}

func clientEDNS(q query.Query) (bool, bool) {
	var p dnsmessage.Parser
	_, err := p.Start(q.Payload)
	if err != nil {
		return false, false
	}
	if err := p.SkipAllQuestions(); err != nil {
		return false, false
	}
	if err := p.SkipAllAnswers(); err != nil {
		return false, false
	}
	if err := p.SkipAllAuthorities(); err != nil {
		return false, false
	}
	for {
		rh, err := p.AdditionalHeader()
		if err == dnsmessage.ErrSectionDone {
			return false, false
		}
		if err != nil {
			return false, false
		}
		if rh.Type != dnsmessage.TypeOPT {
			if err := p.SkipAdditional(); err != nil {
				return false, false
			}
			continue
		}
		return true, rh.TTL&0x8000 != 0
	}
}

// appendOPT tries progressively smaller complete OPT records. Padding and EDE
// are disposable first; COOKIE and ECS are retained whenever they fit.
func appendOPT(base []byte, h dnsmessage.ResourceHeader, opt dnsmessage.OPTResource, limit int) ([]byte, bool, bool) {
	for pass := 0; pass < 4; pass++ {
		options := opt.Options
		dropped, nonPadding := false, false
		if pass > 0 {
			options = make([]dnsmessage.Option, 0, len(opt.Options))
			for _, o := range opt.Options {
				keep := pass == 1 && o.Code != 12 || pass == 2 && o.Code != 12 && o.Code != 15 || pass == 3 && (o.Code == 8 || o.Code == 10)
				if keep {
					options = append(options, o)
				} else {
					dropped = true
					// RFC 8914 section 3 recommends TC when dropping EDE.
					if o.Code != 12 {
						nonPadding = true
					}
				}
			}
		}
		size := len(base) + 11
		for _, o := range options {
			size += 4 + len(o.Data)
		}
		if size <= limit {
			return encodeOPT(base, h, options), dropped && nonPadding, true
		}
	}
	return nil, false, false
}

func encodeOPT(base []byte, h dnsmessage.ResourceHeader, options []dnsmessage.Option) []byte {
	n := len(base) + 11
	for _, o := range options {
		n += 4 + len(o.Data)
	}
	msg := make([]byte, len(base), n)
	copy(msg, base)
	msg = append(msg, 0, 0, 41, byte(h.Class>>8), byte(h.Class), byte(h.TTL>>24), byte(h.TTL>>16), byte(h.TTL>>8), byte(h.TTL), 0, 0)
	rdlen := 0
	for _, o := range options {
		msg = append(msg, byte(o.Code>>8), byte(o.Code), byte(len(o.Data)>>8), byte(len(o.Data)))
		msg = append(msg, o.Data...)
		rdlen += 4 + len(o.Data)
	}
	msg[len(base)+9], msg[len(base)+10] = byte(rdlen>>8), byte(rdlen)
	ar := uint16(msg[10])<<8 | uint16(msg[11])
	ar++
	msg[10], msg[11] = byte(ar>>8), byte(ar)
	return msg
}

func dnsSectionEnds(msg []byte) (questionEnd, authorityEnd int, err error) {
	if len(msg) < 12 {
		return 0, 0, errors.New("short DNS header")
	}
	off := 12
	qd := int(msg[4])<<8 | int(msg[5])
	an := int(msg[6])<<8 | int(msg[7])
	ns := int(msg[8])<<8 | int(msg[9])
	ar := int(msg[10])<<8 | int(msg[11])
	for i := 0; i < qd; i++ {
		off, err = skipWireName(msg, off)
		if err != nil || off+4 > len(msg) {
			return 0, 0, errors.New("invalid question")
		}
		off += 4
	}
	questionEnd = off
	for i := 0; i < an+ns; i++ {
		off, err = skipWireRR(msg, off)
		if err != nil {
			return 0, 0, err
		}
	}
	authorityEnd = off
	for i := 0; i < ar; i++ {
		off, err = skipWireRR(msg, off)
		if err != nil {
			return 0, 0, err
		}
	}
	if off != len(msg) {
		return 0, 0, errors.New("trailing DNS bytes")
	}
	return questionEnd, authorityEnd, nil
}

// requiredGlue retains in-domain addresses in referrals (RFC 9471). Other
// additional data may be omitted without setting TC (RFC 2181 section 9).
// Re-encode these A/AAAA records without compression: their old owner-name
// pointers may reference additional records that are being removed.
func requiredGlue(msg dnsmessage.Message) ([]byte, error) {
	if msg.RCode != dnsmessage.RCodeSuccess || !referralAnswers(msg) {
		return nil, nil
	}
	type key struct {
		name  string
		class dnsmessage.Class
	}
	targets := make(map[key]bool)
	for _, rr := range msg.Authorities {
		if rr.Header.Type == dnsmessage.TypeSOA {
			return nil, nil // Negative answer, not a referral.
		}
		if ns, ok := rr.Body.(*dnsmessage.NSResource); ok {
			zone, name := strings.ToLower(rr.Header.Name.String()), strings.ToLower(ns.NS.String())
			if zone == "." || name == zone || strings.HasSuffix(name, "."+zone) {
				targets[key{name, rr.Header.Class}] = true
			}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
	if err := b.StartAdditionals(); err != nil {
		return nil, err
	}
	for _, rr := range msg.Additionals {
		if !targets[key{strings.ToLower(rr.Header.Name.String()), rr.Header.Class}] {
			continue
		}
		var err error
		switch r := rr.Body.(type) {
		case *dnsmessage.AResource:
			err = b.AResource(rr.Header, *r)
		case *dnsmessage.AAAAResource:
			err = b.AAAAResource(rr.Header, *r)
		}
		if err != nil {
			return nil, err
		}
	}
	return b.Finish()
}

// A referral may follow aliases in the answer section (RFC 1034, 6.2.7).
// A complete answer can also carry authority NS records, whose addresses
// remain optional. Distinguish those cases before retaining referral glue.
func referralAnswers(msg dnsmessage.Message) bool {
	if len(msg.Answers) == 0 {
		return true
	}
	if len(msg.Questions) != 1 {
		return false
	}
	q := msg.Questions[0]
	if q.Type == dnsmessage.TypeCNAME || q.Type == dnsmessage.TypeALL {
		return false
	}
	aliases := make(map[string]string)
	for _, rr := range msg.Answers {
		if c, ok := rr.Body.(*dnsmessage.CNAMEResource); ok && rr.Header.Class == q.Class {
			aliases[strings.ToLower(rr.Header.Name.String())] = strings.ToLower(c.CNAME.String())
		}
	}
	name := strings.ToLower(q.Name.String())
	for i := 0; i < len(aliases); i++ {
		target, ok := aliases[name]
		if !ok {
			break
		}
		name = target
	}
	alias := false
	for _, rr := range msg.Answers {
		if rr.Header.Type == q.Type && rr.Header.Class == q.Class && strings.ToLower(rr.Header.Name.String()) == name {
			return false // The requested terminal RR is present.
		}
		switch rr.Header.Type {
		case dnsmessage.TypeCNAME, 39: // DNAME (with its synthesized CNAME).
			alias = true
		case 46: // RRSIGs may accompany an alias chain.
		default:
			return false
		}
	}
	return alias
}

func skipWireName(msg []byte, off int) (int, error) {
	for {
		if off >= len(msg) {
			return 0, errors.New("short DNS name")
		}
		l := msg[off]
		switch l & 0xc0 {
		case 0xc0:
			if off+2 > len(msg) {
				return 0, errors.New("short DNS pointer")
			}
			return off + 2, nil
		case 0:
			off++
			if l == 0 {
				return off, nil
			}
			if l > 63 || off+int(l) > len(msg) {
				return 0, errors.New("invalid DNS label")
			}
			off += int(l)
		default:
			return 0, errors.New("invalid DNS label")
		}
	}
}

func skipWireRR(msg []byte, off int) (int, error) {
	var err error
	if off, err = skipWireName(msg, off); err != nil {
		return 0, err
	}
	if off+10 > len(msg) {
		return 0, errors.New("short DNS resource header")
	}
	rdlen := int(msg[off+8])<<8 | int(msg[off+9])
	off += 10
	if off+rdlen > len(msg) {
		return 0, errors.New("short DNS resource data")
	}
	return off + rdlen, nil
}
