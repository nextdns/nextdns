package proxy

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/nextdns/nextdns/internal/dnsmessage"
	"github.com/nextdns/nextdns/resolver"
	"github.com/nextdns/nextdns/resolver/query"
)

func replyRCode(rcode dnsmessage.RCode, q query.Query, buf []byte) (n int) {
	b := dnsmessage.NewBuilder(buf[:0], dnsmessage.Header{
		ID:       q.ID,
		Response: true,
		RCode:    rcode,
	})
	_ = b.StartQuestions()
	name, _ := dnsmessage.NewName(q.Name)
	_ = b.Question(dnsmessage.Question{
		Class: dnsmessage.Class(q.Class),
		Type:  dnsmessage.Type(q.Type),
		Name:  name,
	})
	buf, _ = b.Finish()
	return len(buf)
}

func isNXDomain(msg []byte) bool {
	if len(msg) < 4 {
		return false
	}
	const rCodeNXDomain = 3
	return msg[3]&0xf == rCodeNXDomain
}

func hostsResolve(r HostResolver, q query.Query, buf []byte) (n int, i resolver.ResolveInfo, err error) {
	var rrs []string
	var found bool
	switch q.Type {
	case query.TypeA:
		for _, ip := range r.LookupHost(q.Name) {
			found = true
			if strings.IndexByte(ip, '.') != -1 {
				rrs = append(rrs, ip)
			}
		}
	case query.TypeAAAA:
		for _, ip := range r.LookupHost(q.Name) {
			found = true
			if strings.IndexByte(ip, '.') == -1 {
				rrs = append(rrs, ip)
			}
		}
	case query.TypePTR:
		for _, host := range r.LookupAddr(ptrIP(q.Name).String()) {
			found = true
			if strings.HasSuffix(host, ".") {
				rrs = append(rrs, host)
			}
		}
	default:
		// Make sure we don't send a NXDOMAIN for any other qtype if an entry
		// exists in the hosts file.
		found = len(r.LookupHost(q.Name)) > 0
	}
	if !found {
		err = errors.New("not found")
		return
	}

	var p dnsmessage.Parser
	h, err := p.Start(q.Payload)
	if err != nil {
		return 0, i, err
	}
	q1, err := p.Question()
	if err != nil {
		return 0, i, err
	}
	h.Response = true
	h.RCode = dnsmessage.RCodeSuccess
	h.RecursionAvailable = true
	b := dnsmessage.NewBuilder(buf[:0], h)
	_ = b.StartQuestions()
	_ = b.Question(q1)
	_ = b.StartAnswers()
	hdr := dnsmessage.ResourceHeader{
		Name:  q1.Name,
		Type:  q1.Type,
		Class: q1.Class,
		TTL:   0,
	}
	for _, rr := range rrs {
		switch q.Type {
		case query.TypeA:
			if ip := net.ParseIP(rr).To4(); len(ip) == 4 {
				var a [4]byte
				copy(a[:], ip[:4])
				err = b.AResource(hdr, dnsmessage.AResource{A: a})
			}
		case query.TypeAAAA:
			if ip := net.ParseIP(rr); len(ip) == 16 {
				var aaaa [16]byte
				copy(aaaa[:], ip[:16])
				err = b.AAAAResource(hdr, dnsmessage.AAAAResource{AAAA: aaaa})
			}
		case query.TypePTR:
			var ptr dnsmessage.Name
			if ptr, err = dnsmessage.NewName(rr); err != nil {
				return
			}
			err = b.PTRResource(hdr, dnsmessage.PTRResource{PTR: ptr})
		}
	}
	if err != nil {
		return
	}

	buf, err = b.Finish()
	return len(buf), i, err
}

func isPrivateReverse(qname string) bool {
	if ip := ptrIP(qname); ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return true
		}
		if ip := ip.To4(); ip != nil {
			return (ip[0] == 10) ||
				(ip[0] == 172 && ip[1]&0xf0 == 16) ||
				(ip[0] == 192 && ip[1] == 168)
		}
		return ip[0] == 0xfd
	}
	return false
}

func ptrIP(ptr string) net.IP {
	if !strings.HasSuffix(ptr, ".arpa.") {
		return nil
	}
	ptr = ptr[:len(ptr)-6]
	var l int
	var base int
	if strings.HasSuffix(ptr, ".in-addr") {
		ptr = ptr[:len(ptr)-8]
		l = net.IPv4len
		base = 10
	} else if strings.HasSuffix(ptr, ".ip6") {
		ptr = ptr[:len(ptr)-4]
		l = net.IPv6len
		base = 16
	}
	if l == 0 {
		return nil
	}
	ip := make(net.IP, l)
	if base == 16 {
		l *= 2
	}
	for i := 0; i < l && ptr != ""; i++ {
		idx := strings.LastIndexByte(ptr, '.')
		off := idx + 1
		if idx == -1 {
			idx = 0
			off = 0
		} else if idx == len(ptr)-1 {
			return nil
		}
		n, err := strconv.ParseUint(ptr[off:], base, 8)
		if err != nil {
			return nil
		}
		b := byte(n)
		ii := i
		if base == 16 {
			// ip6 use hex nibbles instead of base 10 bytes, so we need to join
			// nibbles by two.
			ii /= 2
			if i&1 == 1 {
				b |= ip[ii] << 4
			}
		}
		ip[ii] = b
		ptr = ptr[:idx]
	}
	return ip
}

func addrIP(addr net.Addr) (ip net.IP) {
	// Avoid parsing/alloc when it's an IP already.
	switch addr := addr.(type) {
	case *net.IPAddr:
		ip = addr.IP
	case *net.UDPAddr:
		ip = addr.IP
	case *net.TCPAddr:
		ip = addr.IP
	default:
		host, _, _ := net.SplitHostPort(addr.String())
		ip = net.ParseIP(host)
	}
	return
}
