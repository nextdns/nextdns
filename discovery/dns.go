package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/internal/dnsmessage"
)

func (r *Resolver) startDNS(ctx context.Context, entries chan entry) error {
	var dns []string
	for _, ip := range host.DNS() {
		// Only consider sending local IP PTR to private DNS.
		if isPrivateIP(ip) {
			dns = append(dns, ip)
		}
	}
	if len(dns) == 0 {
		return nil
	}
	negCacheCreated := time.Now()
	negCache := map[string]struct{}{}
	missCh := make(chan string, 10)
	r.miss = func(addr string) {
		select {
		case missCh <- addr:
		default:
		}
	}

	go func() {
		for {
			select {
			case addr := <-missCh:
				if _, found := negCache[addr]; found {
					if time.Since(negCacheCreated) > 5*time.Minute {
						negCacheCreated = time.Now()
						negCache = map[string]struct{}{}
					}
					continue
				}
				ip := net.ParseIP(addr)
				if ip == nil {
					// Most likely a MAC
					negCache[addr] = struct{}{}
					continue
				}
				if name, err := queryPTR(dns[0], ip); err == nil {
					entries <- entry{sourceDNS, addr, name}
				} else if r.WarnLog != nil {
					negCache[addr] = struct{}{}
					r.WarnLog(fmt.Sprintf("dns: %s->%s: %v", addr, dns[0], err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func isPrivateIP(ip string) bool {
	if ip := net.ParseIP(ip); ip != nil {
		if ip := ip.To4(); ip != nil {
			return (ip[0] == 10) ||
				(ip[0] == 172 && ip[1]&0xf0 == 16) ||
				(ip[0] == 192 && ip[1] == 168)
		}
		return ip[0] == 0xfd
	}
	return false
}

func queryPTR(dns string, ip net.IP) (string, error) {
	c, err := net.Dial("udp", net.JoinHostPort(dns, "53"))
	if err != nil {
		return "", err
	}
	if err = c.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return "", err
	}
	buf := make([]byte, 0, 514)
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{})
	b.EnableCompression()
	_ = b.StartQuestions()
	arpa, err := dnsmessage.NewName(reverseIP(ip))
	if err != nil {
		return "", err
	}
	_ = b.Question(dnsmessage.Question{
		Class: dnsmessage.ClassINET,
		Type:  dnsmessage.TypePTR,
		Name:  arpa,
	})
	buf, err = b.Finish()
	if err != nil {
		return "", err
	}
	_, err = c.Write(buf)
	if err != nil {
		return "", err
	}
	n, err := c.Read(buf[:514])
	if err != nil {
		return "", err
	}
	var p dnsmessage.Parser
	if _, err := p.Start(buf[:n]); err != nil {
		return "", err
	}
	_ = p.SkipAllQuestions()
	for {
		h, err := p.AnswerHeader()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return "", err
		}
		if h.Type != dnsmessage.TypePTR {
			continue
		}
		r, err := p.PTRResource()
		if err != nil {
			return "", err
		}
		return r.PTR.String(), nil
	}
	return "", errors.New("not found")
}

// reverseIP returns the in-addr.arpa. or ip6.arpa. hostname of the IP address
// addr suitable for rDNS (PTR) record lookup or an error if it fails to parse
// the IP address.
func reverseIP(ip net.IP) string {
	const hexDigit = "0123456789abcdef"

	if ip.To4() != nil {
		return uitoa(uint(ip[15])) + "." + uitoa(uint(ip[14])) + "." + uitoa(uint(ip[13])) + "." + uitoa(uint(ip[12])) + ".in-addr.arpa."
	}
	// Must be IPv6
	buf := make([]byte, 0, len(ip)*4+len("ip6.arpa."))
	// Add it, in reverse, to the buffer
	for i := len(ip) - 1; i >= 0; i-- {
		v := ip[i]
		buf = append(buf, hexDigit[v&0xF],
			'.',
			hexDigit[v>>4],
			'.')
	}
	// Append "ip6.arpa." and return (buf already has the final .)
	buf = append(buf, "ip6.arpa."...)
	return string(buf)
}

// Convert unsigned integer to decimal string.
func uitoa(val uint) string {
	if val == 0 { // avoid string allocation
		return "0"
	}
	var buf [20]byte // big enough for 64bit value base 10
	i := len(buf) - 1
	for val >= 10 {
		q := val / 10
		buf[i] = byte('0' + val - q*10)
		i--
		val = q
	}
	// val < 10
	buf[i] = byte('0' + val)
	return string(buf[i:])
}
