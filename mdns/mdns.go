package mdns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/nextdns/nextdns/internal/dnsmessage"
)

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
	}
)

type Resolver struct {
	mu sync.RWMutex
	m  map[string]string

	OnDiscover func(ip, host string)

	WarnLog func(string)
}

type entry struct {
	ip, name string
}

func (r *Resolver) Start(ctx context.Context) error {
	ifs, err := multicastInterfaces()
	if err != nil {
		return err
	}
	if len(ifs) == 0 {
		err = errors.New("no interface found")
	}

	entries := make(chan entry)

	var conns []*net.UDPConn
	for _, iface := range ifs {
		var conn *net.UDPConn
		if conn, err = net.ListenMulticastUDP("udp4", &iface, ipv4Addr); err == nil {
			go r.read(ctx, conn, entries)
			conns = append(conns, conn)
		}
		if conn, err = net.ListenMulticastUDP("udp6", &iface, ipv6Addr); err == nil {
			go r.read(ctx, conn, entries)
			conns = append(conns, conn)
		}
	}
	if len(conns) == 0 {
		return err
	}

	go r.run(ctx, entries)

	for {
		if err := r.probe(conns, services); err != nil {
			if err != nil && r.WarnLog != nil {
				r.WarnLog(fmt.Sprintf("probe: %v", err))
			}
			// Probe every second until we succeed
			select {
			case <-time.After(1 * time.Second):
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

	return ctx.Err()
}

func (r *Resolver) Lookup(ip net.IP) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[ip.String()]
}

func (c *Resolver) probe(conns []*net.UDPConn, services []string) error {
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

func (r *Resolver) run(ctx context.Context, ch chan entry) {
	for entry := range ch {
		r.mu.Lock()
		if r.m == nil {
			r.m = map[string]string{}
		}
		name := entry.name
		if idx := strings.IndexByte(name, '.'); idx != -1 {
			name = name[:idx] // remove .local. suffix
		}
		if r.m[entry.ip] != name {
			r.m[entry.ip] = name
			r.mu.Unlock()
			if r.OnDiscover != nil {
				r.OnDiscover(entry.ip, name)
			}
		} else {
			r.mu.Unlock()
		}
	}
}

func (r *Resolver) read(ctx context.Context, conn *net.UDPConn, ch chan entry) {
	defer conn.Close()
	buf := make([]byte, 65536)
	for {
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if err, ok := err.(*net.OpError); ok {
				if err.Timeout() || err.Temporary() {
					continue
				}
			}
			return
		}
		entries, err := parseEntries(buf[:n])
		if err != nil && r.WarnLog != nil {
			r.WarnLog(fmt.Sprintf("parseEntries: %v", err))
		}
		for _, e := range entries {
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}
}

func parseEntries(buf []byte) (entries []entry, err error) {
	var p dnsmessage.Parser
	if _, err = p.Start(buf); err != nil {
		return nil, err
	}
	if err = p.SkipAllQuestions(); err != nil {
		return nil, fmt.Errorf("SkipAllQuestions: %w", err)
	}
	if err = p.SkipAllAnswers(); err != nil {
		return nil, fmt.Errorf("SkipAllAnswers: %w", err)
	}
	if err = p.SkipAllAuthorities(); err != nil {
		return nil, fmt.Errorf("SkipAllAuthorities: %w", err)
	}
	for {
		rh, err := p.AdditionalHeader()
		if err != nil {
			if !errors.Is(err, dnsmessage.ErrSectionDone) {
				return nil, fmt.Errorf("AdditionalHeader: %w", err)
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
			entries = append(entries, entry{net.IP(rr.A[:]).String(), qname})
		case dnsmessage.TypeAAAA:
			rr, err := p.AAAAResource()
			if err != nil {
				return nil, fmt.Errorf("AAAAResource: %w", err)
			}
			qname := rh.Name.String()
			entries = append(entries, entry{net.IP(rr.AAAA[:]).String(), qname})
		default:
			if err = p.SkipAdditional(); err != nil && !errors.Is(err, dnsmessage.ErrSectionDone) {
				return nil, fmt.Errorf("SkipResource: %w", err)
			}
		}
	}
	return entries, err
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
