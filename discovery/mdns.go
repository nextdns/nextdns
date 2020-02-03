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
	mu sync.RWMutex
	m  map[string]string
}

func (r *MDNS) Start(ctx context.Context) error {
	ifs, err := multicastInterfaces()
	if err != nil {
		return err
	}
	if len(ifs) == 0 {
		return errors.New("no interface found")
	}

	var conns []*net.UDPConn
	for _, iface := range ifs {
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
	if len(conns) == 0 {
		return err
	}

	go func() {
		t := TraceFromCtx(ctx)
		backoff := 100 * time.Millisecond
		maxBackoff := 30 * time.Second
		for {
			if err := r.probe(conns, services); err != nil && !isErrNetUnreachableOrInvalid(err) {
				if err != nil && t.OnWarning != nil {
					t.OnWarning(fmt.Sprintf("probe: %v", err))
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

func (r *MDNS) Lookup(addr string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name, found := r.m[addr]
	return name, found
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
	t := TraceFromCtx(ctx)
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
		entries, err := parseEntries(buf[:n])
		if err != nil && t.OnWarning != nil {
			t.OnWarning(fmt.Sprintf("parseEntries: %v", err))
		}
		for addr, name := range entries {
			if isValidName(name) {
				r.mu.Lock()
				if r.m[addr] != name {
					if r.m == nil {
						r.m = map[string]string{}
					}
					r.m[addr] = name
					r.mu.Unlock()
					if t.OnDiscover != nil {
						t.OnDiscover(addr, name, "MDNS")
					}
				} else {
					r.mu.Unlock()
				}
			}
		}
	}
}

const (
	sectionAnswer = iota
	sectionAdditional
)

func parseEntries(buf []byte) (entries map[string]string, err error) {
	var p dnsmessage.Parser
	if _, err = p.Start(buf); err != nil {
		return nil, err
	}
	if err = p.SkipAllQuestions(); err != nil {
		return nil, fmt.Errorf("SkipAllQuestions: %w", err)
	}
	sec := sectionAnswer
	entries = map[string]string{}
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
			entries[net.IP(rr.A[:]).String()] = normalizeName(qname)
		case dnsmessage.TypeAAAA:
			rr, err := p.AAAAResource()
			if err != nil {
				return nil, fmt.Errorf("AAAAResource: %w", err)
			}
			qname := rh.Name.String()
			entries[net.IP(rr.AAAA[:]).String()] = normalizeName(qname)
		default:
			if err = skipRecord(&p, sec); err != nil && !errors.Is(err, dnsmessage.ErrSectionDone) {
				return nil, fmt.Errorf("SkipResource: %w", err)
			}
		}
	}
	return entries, err
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
