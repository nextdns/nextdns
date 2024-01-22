package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/nextdns/nextdns/internal/dnsmessage"
)

const mdnsMaxEntries = 1000

var (
	// mDNS endpoint addresses
	ipv4Addr = &net.UDPAddr{
		IP:   net.IPv4(224, 0, 0, 251),
		Port: 5353,
	}
	ipv6Addr = &net.UDPAddr{
		IP:   net.ParseIP("ff02::fb"),
		Port: 5353,
	}

	// Known services
	services = []string{
		"_hap._tcp.local.",
		"_homekit._tcp.local.",
		"_airplay._tcp.local.",
		"_raop._tcp.local.",
		"_sleep-proxy._udp.local.",
		"_companion-link._tcp.local.",
		"_googlezone._tcp.local.",
		"_googlerpc._tcp.local.",
		"_googlecast._tcp.local.",
		"_http._tcp.local.",
		"_https._tcp.local.",
	}
)

type MDNS struct {
	OnError func(err error)

	mu    sync.RWMutex
	addrs map[string]mdnsEntry
	names map[string]mdnsEntry
}

type mdnsEntry struct {
	lastUpdate time.Time
	values     []string
}

func (r *MDNS) Start(ctx context.Context, filter string) error {
	if filter == "disabled" {
		return nil
	}
	ifs, err := multicastInterfaces()
	if err != nil {
		return err
	}
	if len(ifs) == 0 {
		return errors.New("no interface found")
	}

	var conns []*net.UDPConn
	found := false
	for _, iface := range ifs {
		if filter != "all" && iface.Name != filter {
			continue
		}
		found = true
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		if len(addrs) == 0 {
			continue
		}
		var conn *net.UDPConn
		if conn, err = net.ListenMulticastUDP("udp4", &iface, ipv4Addr); err == nil {
			go r.read(ctx, conn)
			conns = append(conns, conn)
		}
		if conn, err = net.ListenMulticastUDP("udp6", &iface, ipv6Addr); err == nil {
			go r.read(ctx, conn)
			conns = append(conns, conn)
		}
	}
	if !found {
		return fmt.Errorf("unknown interface: %s", filter)
	}
	if len(conns) == 0 {
		return err
	}

	go func() {
		backoff := 100 * time.Millisecond
		maxBackoff := 30 * time.Second
		for {
			if err := r.probe(conns, services); err != nil && !isErrNetUnreachableOrInvalid(err) {
				if err != nil && r.OnError != nil {
					r.OnError(fmt.Errorf("probe: %v", err))
				}
				// Probe every second until we succeed
				select {
				case <-time.After(backoff):
					backoff <<= 1
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					continue
				case <-ctx.Done():
				}
			}
			break
		}

		<-ctx.Done()
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()

	return nil
}

func (r *MDNS) Name() string {
	return "mdns"
}

func (r *MDNS) Visit(f func(name string, addrs []string)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, addrs := range r.names {
		f(name, addrs.values)
	}
}

func (r *MDNS) LookupAddr(addr string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.addrs[addr].values
}

func (r *MDNS) LookupHost(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.names[prepareHostLookup(name)].values
}

func isErrNetUnreachableOrInvalid(err error) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if sysErr, ok := err.(*os.SyscallError); ok {
			return sysErr.Err == syscall.ENETUNREACH || sysErr.Err == syscall.EINVAL
		}
	}
	return false
}

func (c *MDNS) probe(conns []*net.UDPConn, services []string) error {
	buf := make([]byte, 0, 514)
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{})
	b.EnableCompression()
	var err error
	if err = b.StartQuestions(); err != nil {
		return fmt.Errorf("start question: %v", err)
	}
	qt := dnsmessage.Question{
		Class: dnsmessage.ClassINET,
		Type:  dnsmessage.TypePTR,
	}
	for _, service := range services {
		qt.Name = dnsmessage.MustNewName(service)
		err = b.Question(qt)
		if err != nil {
			return fmt.Errorf("PTR %s: %v", service, err)
		}
	}
	if buf, err = b.Finish(); err != nil {
		return err
	}
	for _, conn := range conns {
		addr := ipv4Addr
		if udpAddr, ok := conn.RemoteAddr().(*net.UDPAddr); ok && udpAddr.IP.To4() == nil {
			addr = ipv6Addr
		}
		if _, e := conn.WriteTo(buf, addr); e != nil {
			err = e
		}
	}
	return err
}

func (r *MDNS) read(ctx context.Context, conn *net.UDPConn) {
	defer conn.Close()
	buf := make([]byte, 65536)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if err, ok := err.(*net.OpError); ok {
				if err.Timeout() || err.Temporary() {
					continue
				}
			}
			return
		}
		if n < 12 {
			// Silently ignore obviously invalid messages
			continue
		}
		entries, err := parseEntries(buf[:n])
		if err != nil {
			continue
		}
		r.mu.Lock()
		for addr, name := range entries {
			if isValidName(name) {
				if r.addrs == nil {
					r.addrs = map[string]mdnsEntry{}
					r.names = map[string]mdnsEntry{}
				}
				name := absDomainName([]byte(name))
				h := []byte(name)
				lowerASCIIBytes(h)
				key := absDomainName(h)
				addEntry(r.addrs, addr, name)
				addEntry(r.names, key, addr)
				for len(r.names) > mdnsMaxEntries {
					r.removeOldestEntry()
				}
			}
		}
		r.mu.Unlock()
	}
}

func (r *MDNS) removeOldestEntry() {
	var oldestName string
	oldestTime := time.Now()
	for k, v := range r.names {
		if v.lastUpdate.Before(oldestTime) {
			oldestTime = v.lastUpdate
			oldestName = k
		}
	}
	if oldestName != "" {
		addrs := r.addrs[oldestName].values
		delete(r.names, oldestName)
		for _, addr := range addrs {
			removeEntry(r.addrs, addr, oldestName)
		}
	}
}

const (
	sectionAnswer = iota
	sectionAdditional
)

func parseEntries(buf []byte) (map[string]string, error) {
	var p dnsmessage.Parser
	if _, err := p.Start(buf); err != nil {
		return nil, err
	}
	if err := p.SkipAllQuestions(); err != nil {
		return nil, fmt.Errorf("SkipAllQuestions: %w", err)
	}
	sec := sectionAnswer
	entries := map[string]string{}
	for {
		rh, err := getHeader(&p, sec)
		if err != nil {
			if !errors.Is(err, dnsmessage.ErrSectionDone) {
				return nil, fmt.Errorf("AdditionalHeader: %w", err)
			}
			if sec == sectionAnswer {
				sec = sectionAdditional
				if err = p.SkipAllAuthorities(); err != nil {
					return nil, fmt.Errorf("SkipAllAuthorities: %w", err)
				}
				continue
			}
			break
		}
		switch rh.Type {
		case dnsmessage.TypeA:
			rr, err := p.AResource()
			if err != nil {
				return nil, fmt.Errorf("AResource: %w", err)
			}
			qname := rh.Name.String()
			entries[net.IP(rr.A[:]).String()] = qname
		case dnsmessage.TypeAAAA:
			rr, err := p.AAAAResource()
			if err != nil {
				return nil, fmt.Errorf("AAAAResource: %w", err)
			}
			qname := rh.Name.String()
			entries[net.IP(rr.AAAA[:]).String()] = qname
		default:
			if err = skipRecord(&p, sec); err != nil && !errors.Is(err, dnsmessage.ErrSectionDone) {
				return nil, fmt.Errorf("SkipResource: %w", err)
			}
		}
	}
	return entries, nil
}

func addEntry(entries map[string]mdnsEntry, key, value string) {
	entry := entries[key]
	entry.values = appendUniq(entry.values, value)
	entry.lastUpdate = time.Now()
	entries[key] = entry
}

func removeEntry(entries map[string]mdnsEntry, key, value string) {
	entry := entries[key]
	for i, v := range entry.values {
		if v == value {
			entry.values = append(entry.values[:i], entry.values[i+1:]...)
			break
		}
	}
	if len(entry.values) == 0 {
		delete(entries, key)
	} else {
		entries[key] = entry
	}
}

func getHeader(p *dnsmessage.Parser, sec int) (dnsmessage.ResourceHeader, error) {
	switch sec {
	case sectionAnswer:
		return p.AnswerHeader()
	case sectionAdditional:
		return p.AdditionalHeader()
	}
	return dnsmessage.ResourceHeader{}, errors.New("invalid section")
}

func skipRecord(p *dnsmessage.Parser, t int) error {
	switch t {
	case sectionAnswer:
		return p.SkipAnswer()
	case sectionAdditional:
		return p.SkipAdditional()
	}
	return errors.New("invalid section")
}

func multicastInterfaces() ([]net.Interface, error) {
	var interfaces []net.Interface
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, ifi := range ifaces {
		if (ifi.Flags & net.FlagUp) == 0 {
			continue
		}
		if (ifi.Flags & net.FlagMulticast) > 0 {
			interfaces = append(interfaces, ifi)
		}
	}
	return interfaces, nil
}
