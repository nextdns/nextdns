package discovery

import (
	"crypto/rand"
	"net"
	"slices"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/internal/dnsmessage"
)

type DNS struct {
	Upstream string

	cache *lru.Cache[string, cacheEntry]
	once  sync.Once

	antiLoop semaphoreMap
	rd       bool
}

type cacheEntry struct {
	Values []string
	Expiry time.Time
}

func (r *DNS) init() {
	defer func() {
		// Probe the upstream in order to detect if it's a buggy dnsmasq version
		// returning SERVFAILs when RD flag is not provided. For those, we send
		// the RD flag at the risk of creating DNS loops.
		r.rd = probeBuggyDNSMasq(r.Upstream)
	}()
	r.cache, _ = lru.New[string, cacheEntry](10000)
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
		values, found := r.cacheGet(key)
		if found {
			f(key, values)
		}
	}
}

func (r *DNS) LookupAddr(addr string) []string {
	return r.runSingle(r.lookupAddr, addr)
}

func (r *DNS) lookupAddr(addr string) []string {
	r.once.Do(r.init)
	if r.Upstream == "" {
		return nil
	}
	names, found := r.cacheGet(addr)
	if found {
		return names
	}
	names, _ = queryPTR(r.Upstream, net.ParseIP(addr), r.rd)
	names = slices.DeleteFunc(names, func(s string) bool { return !isValidName(s) })
	if names == nil {
		names = []string{}
	}
	r.cacheSet(addr, names)
	return names
}

func (r *DNS) LookupHost(name string) []string {
	return r.runSingle(r.lookupHost, name)
}

func (r *DNS) lookupHost(name string) []string {
	r.once.Do(r.init)
	if r.Upstream == "" {
		return nil
	}
	addrs, found := r.cacheGet(name)
	if found {
		return addrs
	}
	var a []string
	done := make(chan struct{})
	go func() {
		a, _ = queryName(r.Upstream, name, dnsmessage.TypeA, r.rd)
		close(done)
	}()
	aaaa, _ := queryName(r.Upstream, name, dnsmessage.TypeAAAA, r.rd)
	<-done
	addrs = append(a, aaaa...)
	r.cacheSet(name, addrs)
	return addrs
}

// runSingle prevents DNS loops by making sure only one query for a given entity
// is in flight at a time. Any subsequent query (potentially resulting from a
// loop) will return an empty response, breaking the loop.
func (r *DNS) runSingle(f func(string) []string, arg string) []string {
	acquired := r.antiLoop.Acquire(arg)
	defer r.antiLoop.Release(arg)
	if acquired {
		return f(arg)
	}
	return nil
}

func (r *DNS) cacheGet(key string) (rrs []string, found bool) {
	e, ok := r.cache.Get(key)
	if !ok {
		return nil, false
	}
	if time.Now().After(e.Expiry) {
		return nil, false
	}
	return e.Values, true
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
	New: func() any {
		b := new(dnsMsgBuf)
		return b
	},
}

type dnsMsgBuf [514]byte

func queryPTR(dns string, ip net.IP, rd bool) ([]string, error) {
	bp := bufPool.Get().(*dnsMsgBuf)
	defer bufPool.Put(bp)
	buf := bp[:0]
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{
		RecursionDesired: rd,
	})
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
	buf, err = b.Finish()
	if err != nil {
		return nil, err
	}
	return sendQuery(dns, buf, dnsmessage.TypePTR)
}

type dnsError dnsmessage.RCode

func (err dnsError) Error() string {
	return err.RCode().String()
}

func (err dnsError) RCode() dnsmessage.RCode {
	return dnsmessage.RCode(err)
}

func queryName(dns, name string, typ dnsmessage.Type, rd bool) ([]string, error) {
	bp := bufPool.Get().(*dnsMsgBuf)
	defer bufPool.Put(bp)
	buf := bp[:0]
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{
		RecursionDesired: rd,
	})
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
	buf, err = b.Finish()
	if err != nil {
		return nil, err
	}
	return sendQuery(dns, buf, typ)
}

func sendQuery(dns string, buf []byte, typ dnsmessage.Type) (rrs []string, err error) {
	host, port, err := net.SplitHostPort(dns)
	if err != nil {
		host = dns
		port = "53"
	}
	c, err := net.Dial("udp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if err = c.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return nil, err
	}
	// Create a random msg id
	if _, err := rand.Read(buf[:2]); err != nil {
		return nil, err
	}
	if _, err = c.Write(buf); err != nil {
		return nil, err
	}
	id := uint16(buf[0])<<8 | uint16(buf[1])
	var n int
	for {
		if n, err = c.Read(buf[:514]); err != nil {
			return nil, err
		}
		if n < 2 {
			continue
		}
		if id != uint16(buf[0])<<8|uint16(buf[1]) {
			// Skip mismatch id as it may come from previous timeout query.
			continue
		}
		break
	}
	var p dnsmessage.Parser
	h, err := p.Start(buf[:n])
	if err != nil {
		return nil, err
	}
	if h.RCode != dnsmessage.RCodeSuccess {
		return nil, dnsError(h.RCode)
	}
	if err := p.SkipAllQuestions(); err != nil {
		return nil, err
	}
	for maxRR := 100; maxRR > 0; maxRR-- {
		h, err := p.AnswerHeader()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Type != typ {
			if err := p.SkipAnswer(); err != nil {
				return nil, err
			}
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
		case dnsmessage.TypeAAAA:
			r, err := p.AAAAResource()
			if err != nil {
				return nil, err
			}
			rrs = append(rrs, net.IP(r.AAAA[:]).String())
		default:
			if err := p.SkipAnswer(); err != nil {
				return nil, err
			}
		}
	}
	return rrs, nil
}

// probeBuggyDNSMasq returns true if upstream expose the buggy behavior of
// dnsmasq 2.80 of returning a SERVFAIL on queries without the RD flag.
func probeBuggyDNSMasq(upstream string) bool {
	var wg sync.WaitGroup
	wg.Add(2)
	var errRD, errNoRD error
	go func() {
		_, errNoRD = queryName(upstream, "localhost.", dnsmessage.TypeA, false)
		wg.Done()
	}()
	go func() {
		_, errRD = queryName(upstream, "localhost.", dnsmessage.TypeA, true)
		wg.Done()
	}()
	wg.Wait()
	if errNoRD != nil && errRD == nil {
		if err, ok := errNoRD.(dnsError); ok && err.RCode() == dnsmessage.RCodeServerFailure {
			return true
		}
	}
	return false
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
