package discovery

import (
	"net"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"

	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/internal/dnsmessage"
	"github.com/nextdns/nextdns/resolver/query"
)

type DNS struct {
	Upstream string

	cache *lru.Cache
	once  sync.Once
}

type cacheEntry struct {
	Values []string
	Expiry time.Time
}

func (r *DNS) init() {
	r.cache, _ = lru.New(10000)
	if r.Upstream != "" {
		return
	}
	var servers []string
	for _, ip := range host.DNS() {
		// Only consider sending local IP PTR to private DNS.
		if isPrivateIP(ip) {
			servers = append(servers, ip)
		}
	}
	if len(servers) == 0 {
		return
	}
	r.Upstream = servers[0]
}

func (r *DNS) Name() string {
	return "dns"
}

func (r *DNS) Visit(f func(name string, addrs []string)) {
	r.once.Do(r.init)
	for _, key := range r.cache.Keys() {
		values := r.cacheGet(key.(string))
		if values != nil {
			f(key.(string), values)
		}
	}
}

func (r *DNS) LookupAddr(addr string) []string {
	r.once.Do(r.init)
	if r.Upstream == "" {
		return nil
	}
	names := r.cacheGet(addr)
	if names != nil {
		return names
	}
	names, _ = queryPTR(r.Upstream, net.ParseIP(addr))
	for i, name := range names {
		if isValidName(name) {
			names[i] = name
		}
	}
	if names == nil {
		names = []string{}
	}
	r.cacheSet(addr, names)
	return names
}

func (r *DNS) LookupHost(name string) []string {
	r.once.Do(r.init)
	if r.Upstream == "" {
		return nil
	}
	addrs := r.cacheGet(name)
	if addrs != nil {
		return addrs
	}
	var a []string
	done := make(chan struct{})
	go func() {
		a, _ = queryName(r.Upstream, name, dnsmessage.TypeA)
		close(done)
	}()
	aaaa, _ := queryName(r.Upstream, name, dnsmessage.TypeAAAA)
	<-done
	addrs = append(a, aaaa...)
	r.cacheSet(name, addrs)
	return addrs
}

func (r *DNS) cacheGet(key string) []string {
	v, ok := r.cache.Get(key)
	if !ok {
		return nil
	}
	e, ok := v.(cacheEntry)
	if !ok {
		return nil
	}
	if time.Now().After(e.Expiry) {
		return nil
	}
	return e.Values
}

func (r *DNS) cacheSet(key string, values []string) {
	r.cache.Add(key, cacheEntry{
		Values: values,
		Expiry: time.Now().Add(5 * time.Minute),
	})
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

var bufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 514)
		return &buf
	},
}

func putBufPool(buf []byte) {
	if cap(buf) == 514 {
		buf = buf[:0]
		bufPool.Put(&buf)
	}
}

func queryPTR(dns string, ip net.IP) ([]string, error) {
	buf := *bufPool.Get().(*[]byte)
	defer putBufPool(buf)
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{})
	b.EnableCompression()
	_ = b.StartQuestions()
	arpa, err := dnsmessage.NewName(reverseIP(ip))
	if err != nil {
		return nil, err
	}
	_ = b.Question(dnsmessage.Question{
		Class: dnsmessage.ClassINET,
		Type:  dnsmessage.TypePTR,
		Name:  arpa,
	})
	addEDNS0(&b)
	buf, err = b.Finish()
	if err != nil {
		return nil, err
	}
	return sendQuery(dns, buf, dnsmessage.TypePTR)
}

func queryName(dns, name string, typ dnsmessage.Type) ([]string, error) {
	buf := *bufPool.Get().(*[]byte)
	defer putBufPool(buf)
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{})
	b.EnableCompression()
	_ = b.StartQuestions()
	qname, err := dnsmessage.NewName(name)
	if err != nil {
		return nil, err
	}
	_ = b.Question(dnsmessage.Question{
		Class: dnsmessage.ClassINET,
		Type:  typ,
		Name:  qname,
	})
	addEDNS0(&b)
	buf, err = b.Finish()
	if err != nil {
		return nil, err
	}
	return sendQuery(dns, buf, typ)
}

// addEDNS0 adds a custom NextDNS DNS0 extension to disable discovery in case
// the upstream DNS we are talking to is ourself.
func addEDNS0(b *dnsmessage.Builder) {
	_ = b.StartAdditionals()
	_ = b.OPTResource(dnsmessage.ResourceHeader{
		Type:  dnsmessage.TypeOPT,
		Name:  dnsmessage.MustNewName("."),
		Class: 4096,
	}, dnsmessage.OPTResource{Options: []dnsmessage.Option{{
		Code: query.EDNS0_NEXTDNS,
		Data: []byte{byte(query.NDFlagDisableDiscovery)},
	}}})
}

func sendQuery(dns string, buf []byte, typ dnsmessage.Type) (rrs []string, err error) {
	c, err := net.Dial("udp", net.JoinHostPort(dns, "53"))
	if err != nil {
		return nil, err
	}
	if err = c.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return nil, err
	}
	_, err = c.Write(buf)
	if err != nil {
		return nil, err
	}
	n, err := c.Read(buf[:514])
	if err != nil {
		return nil, err
	}
	var p dnsmessage.Parser
	if _, err := p.Start(buf[:n]); err != nil {
		return nil, err
	}
	_ = p.SkipAllQuestions()
	for {
		h, err := p.AnswerHeader()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Type != typ {
			continue
		}
		switch h.Type {
		case dnsmessage.TypePTR:
			r, err := p.PTRResource()
			if err != nil {
				return nil, err
			}
			rrs = append(rrs, r.PTR.String())
		case dnsmessage.TypeA:
			r, err := p.AResource()
			if err != nil {
				return nil, err
			}
			rrs = append(rrs, net.IP(r.A[:]).String())
		}
	}
	return rrs, nil
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
