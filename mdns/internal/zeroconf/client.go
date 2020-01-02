package zeroconf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
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
)

// IPType specifies the IP traffic the client listens for.
// This does not guarantee that only mDNS entries of this sepcific
// type passes. E.g. typical mDNS packets distributed via IPv4, often contain
// both DNS A and AAAA entries.
type IPType uint8

// Options for IPType.
const (
	IPv4        = 0x01
	IPv6        = 0x02
	IPv4AndIPv6 = (IPv4 | IPv6) //< Default option.
)

type clientOpts struct {
	listenOn IPType
	logWarn  func(string)
}

// ClientOption fills the option struct to configure intefaces, etc.
type ClientOption func(*clientOpts)

// SelectIPTraffic selects the type of IP packets (IPv4, IPv6, or both) this
// instance listens for.
// This does not guarantee that only mDNS entries of this sepcific
// type passes. E.g. typical mDNS packets distributed via IPv4, may contain
// both DNS A and AAAA entries.
func SelectIPTraffic(t IPType) ClientOption {
	return func(o *clientOpts) {
		o.listenOn = t
	}
}

func WarnLogger(f func(string)) ClientOption {
	return func(o *clientOpts) {
		o.logWarn = f
	}
}

// Resolver acts as entry point for service lookups and to browse the DNS-SD.
type Resolver struct {
	c *client
}

// NewResolver creates a new resolver and joins the UDP multicast groups to
// listen for mDNS messages.
func NewResolver(options ...ClientOption) (*Resolver, error) {
	// Apply default configuration and load supplied options.
	var conf = clientOpts{
		listenOn: IPv4AndIPv6,
	}
	for _, o := range options {
		if o != nil {
			o(&conf)
		}
	}

	c, err := newClient(conf)
	if err != nil {
		return nil, err
	}
	return &Resolver{
		c: c,
	}, nil
}

// Browse for all services of a given type in a given domain.
func (r *Resolver) Browse(ctx context.Context, service, domain string, entries chan<- *ServiceEntry) error {
	params := defaultParams(service)
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = entries
	ctx, cancel := context.WithCancel(ctx)
	go r.c.mainloop(ctx, params)

	err := r.c.query(params)
	if err != nil {
		cancel()
		return err
	}
	// If previous probe was ok, it should be fine now. In case of an error later on,
	// the entries' queue is closed.
	go func() {
		if err := r.c.periodicQuery(ctx, params); err != nil {
			cancel()
		}
	}()

	return nil
}

// Lookup a specific service by its name and type in a given domain.
func (r *Resolver) Lookup(ctx context.Context, instance, service, domain string, entries chan<- *ServiceEntry) error {
	params := defaultParams(service)
	params.Instance = instance
	if domain != "" {
		params.Domain = domain
	}
	params.Entries = entries
	ctx, cancel := context.WithCancel(ctx)
	go r.c.mainloop(ctx, params)
	err := r.c.query(params)
	if err != nil {
		// cancel mainloop
		cancel()
		return err
	}
	// If previous probe was ok, it should be fine now. In case of an error later on,
	// the entries' queue is closed.
	go func() {
		if err := r.c.periodicQuery(ctx, params); err != nil {
			cancel()
		}
	}()

	return nil
}

// defaultParams returns a default set of QueryParams.
func defaultParams(service string) *LookupParams {
	return NewLookupParams("", service, "local", make(chan *ServiceEntry))
}

// Client structure encapsulates both IPv4/IPv6 UDP connections.
type client struct {
	conns   []*net.UDPConn
	logWarn func(string)
}

// Client structure constructor
func newClient(opts clientOpts) (*client, error) {
	c := &client{
		logWarn: opts.logWarn,
	}
	var conn *net.UDPConn
	var err error
	for _, iface := range listMulticastInterfaces() {
		if (opts.listenOn & IPv4) > 0 {
			conn, err = net.ListenMulticastUDP("udp4", &iface, ipv4Addr)
			if err != nil {
				continue
			}
			c.conns = append(c.conns, conn)
		}
		if (opts.listenOn & IPv6) > 0 {
			conn, err = net.ListenMulticastUDP("udp6", &iface, ipv6Addr)
			if err != nil {
				continue
			}
			c.conns = append(c.conns, conn)
		}
	}
	return c, err
}

func listMulticastInterfaces() []net.Interface {
	var interfaces []net.Interface
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifi := range ifaces {
		if (ifi.Flags & net.FlagUp) == 0 {
			continue
		}
		if (ifi.Flags & net.FlagMulticast) > 0 {
			interfaces = append(interfaces, ifi)
		}
	}

	return interfaces
}

// Start listeners and waits for the shutdown signal from exit channel
func (c *client) mainloop(ctx context.Context, params *LookupParams) {
	// start listening for responses
	msgCh := make(chan []byte, 32)
	for _, conn := range c.conns {
		go c.recv(ctx, conn, msgCh)
	}

	// Iterate through channels from listeners goroutines
	var entries, sentEntries map[string]*ServiceEntry
	sentEntries = make(map[string]*ServiceEntry)
	for {
		select {
		case <-ctx.Done():
			// Context expired. Notify subscriber that we are done here.
			params.done()
			c.shutdown()
			return
		case msg := <-msgCh:
			var err error
			if entries, err = parseEntries(msg, params); err != nil {
				if c.logWarn != nil {
					c.logWarn(fmt.Sprintf("mdns: Failed to unpack packet: %v", err))
				}
				continue
			}
		}

		if len(entries) > 0 {
			for k, e := range entries {
				if e.TTL == 0 {
					delete(entries, k)
					delete(sentEntries, k)
					continue
				}
				if _, ok := sentEntries[k]; ok {
					continue
				}

				// If this is an DNS-SD query do not throw PTR away.
				// It is expected to have only PTR for enumeration
				if params.ServiceRecord.ServiceTypeName() != params.ServiceRecord.ServiceName() {
					// Require at least one resolved IP address for ServiceEntry
					// TODO: wait some more time as chances are high both will arrive.
					if len(e.AddrIPv4) == 0 && len(e.AddrIPv6) == 0 {
						continue
					}
				}
				// Submit entry to subscriber and cache it.
				// This is also a point to possibly stop probing actively for a
				// service entry.
				params.Entries <- e
				sentEntries[k] = e
				params.disableProbing()
			}
		}
	}
}

func parseEntries(msg []byte, params *LookupParams) (entries map[string]*ServiceEntry, err error) {
	entries = make(map[string]*ServiceEntry)
	v4Map := map[string][]net.IP{}
	v6Map := map[string][]net.IP{}

	var p dnsmessage.Parser
	if _, err := p.Start(msg); err != nil {
		return nil, err
	}
	if err = p.SkipAllQuestions(); err != nil {
		return nil, fmt.Errorf("SkipAllQuestions: %w", err)
	}
	answers := true

	for {
		var rh dnsmessage.ResourceHeader
		if answers {
			if rh, err = p.AnswerHeader(); err != nil {
				if !errors.Is(err, dnsmessage.ErrSectionDone) {
					return nil, fmt.Errorf("AnswerHeader: %w", err)
				}
				answers = false
				if err = p.SkipAllAuthorities(); err != nil {
					return nil, fmt.Errorf("SkipAllAuthorities: %w", err)
				}
			}
		}
		if !answers {
			if rh, err = p.AdditionalHeader(); err != nil {
				if !errors.Is(err, dnsmessage.ErrSectionDone) {
					return nil, fmt.Errorf("AdditionalHeader: %w", err)
				}
				break
			}
		}
		switch rh.Type {
		case dnsmessage.TypePTR:
			rr, err := p.PTRResource()
			if err != nil {
				return nil, fmt.Errorf("PTRResource: %w", err)
			}
			qname := rh.Name.String()
			if params.ServiceName() != qname {
				continue
			}
			ptr := rr.PTR.String()
			if params.ServiceInstanceName() != "" && params.ServiceInstanceName() != ptr {
				continue
			}
			if _, ok := entries[ptr]; !ok {
				entries[ptr] = NewServiceEntry(
					trimDot(strings.Replace(ptr, qname, "", -1)),
					params.Service,
					params.Domain)
			}
			entries[ptr].TTL = rh.TTL
		case dnsmessage.TypeSRV:
			rr, err := p.SRVResource()
			if err != nil {
				return nil, fmt.Errorf("SRVResource: %w", err)
			}
			qname := rh.Name.String()
			if params.ServiceInstanceName() != "" && params.ServiceInstanceName() != qname {
				continue
			} else if !strings.HasSuffix(qname, params.ServiceName()) {
				continue
			}
			if _, ok := entries[qname]; !ok {
				entries[qname] = NewServiceEntry(
					trimDot(strings.Replace(qname, params.ServiceName(), "", 1)),
					params.Service,
					params.Domain)
			}
			entries[qname].HostName = rr.Target.String()
			entries[qname].Port = int(rr.Port)
			entries[qname].TTL = rh.TTL
		case dnsmessage.TypeTXT:
			rr, err := p.TXTResource()
			if err != nil {
				return nil, fmt.Errorf("TXTResource: %w", err)
			}
			qname := rh.Name.String()
			if params.ServiceInstanceName() != "" && params.ServiceInstanceName() != qname {
				continue
			} else if !strings.HasSuffix(qname, params.ServiceName()) {
				continue
			}
			if _, ok := entries[qname]; !ok {
				entries[qname] = NewServiceEntry(
					trimDot(strings.Replace(qname, params.ServiceName(), "", 1)),
					params.Service,
					params.Domain)
			}
			entries[qname].Text = rr.TXT
			entries[qname].TTL = rh.TTL
		case dnsmessage.TypeA:
			rr, err := p.AResource()
			if err != nil {
				return nil, fmt.Errorf("AResource: %w", err)
			}
			qname := rh.Name.String()
			v4Map[qname] = append(v4Map[qname], rr.A[:])
		case dnsmessage.TypeAAAA:
			rr, err := p.AAAAResource()
			if err != nil {
				return nil, fmt.Errorf("AAAAResource: %w", err)
			}
			qname := rh.Name.String()
			v6Map[qname] = append(v6Map[qname], rr.AAAA[:])
		default:
			if answers {
				err = p.SkipAnswer()
			} else {
				err = p.SkipAdditional()
			}
			if err != nil && !errors.Is(err, dnsmessage.ErrSectionDone) {
				return nil, fmt.Errorf("SkipResource: %w", err)
			}
		}
	}
	// Associate IPs in a second round as other fields should be filled by now.
	for _, e := range entries {
		e.AddrIPv4 = v4Map[e.HostName]
		e.AddrIPv6 = v6Map[e.HostName]
	}
	return entries, nil
}

// Shutdown client will close currently open connections and channel implicitly.
func (c *client) shutdown() {
	for _, conn := range c.conns {
		conn.Close()
	}
}

// Data receiving routine reads from connection, unpacks packets into dns.Msg
// structures and sends them to a given msgCh channel
func (c *client) recv(ctx context.Context, conn *net.UDPConn, msgCh chan []byte) {
	buf := make([]byte, 65536)
	var fatalErr error
	for {
		// Handles the following cases:
		// - ReadFrom aborts with error due to closed UDP connection -> causes ctx cancel
		// - ReadFrom aborts otherwise.
		// TODO: the context check can be removed. Verify!
		if ctx.Err() != nil || fatalErr != nil {
			return
		}

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			fatalErr = err
			continue
		}
		msg := make([]byte, n)
		copy(msg, buf[:n])
		select {
		case msgCh <- msg:
			// Submit decoded DNS message and continue.
		case <-ctx.Done():
			// Abort.
			return
		}
	}
}

// periodicQuery sens multiple probes until a valid response is received by
// the main processing loop or some timeout/cancel fires.
// TODO: move error reporting to shutdown function as periodicQuery is called from
// go routine context.
func (c *client) periodicQuery(ctx context.Context, params *LookupParams) error {
	if params.stopProbing == nil {
		return nil
	}

	backoff := 4 * time.Second
	const maxBackoff = 60 * time.Second

	for {
		// Do periodic query.
		if err := c.query(params); err != nil {
			return err
		}
		// Backoff and cancel logic.
		select {
		case <-time.After(backoff):
			if nb := backoff << 1; nb > maxBackoff {
				backoff = maxBackoff
			} else {
				backoff = nb
			}
		case <-params.stopProbing:
			// Chan is closed (or happened in the past).
			// Done here. Received a matching mDNS entry.
			return nil
		case <-ctx.Done():
			return ctx.Err()

		}
	}

}

// Performs the actual query by service name (browse) or service instance name (lookup),
// start response listeners goroutines and loops over the entries channel.
func (c *client) query(params *LookupParams) error {
	var serviceName, serviceInstanceName string
	serviceName = fmt.Sprintf("%s.%s.", trimDot(params.Service), trimDot(params.Domain))
	if params.Instance != "" {
		serviceInstanceName = fmt.Sprintf("%s.%s.", params.Instance, serviceName)
	}

	// send the query
	buf := make([]byte, 0, 514)
	b := dnsmessage.NewBuilder(buf, dnsmessage.Header{})
	b.EnableCompression()
	var err error
	if err = b.StartQuestions(); err != nil {
		return fmt.Errorf("start question: %v", err)
	}
	if serviceInstanceName != "" {
		qt := dnsmessage.Question{
			Class: dnsmessage.ClassINET,
			Name:  dnsmessage.MustNewName(serviceInstanceName),
		}
		qt.Type = dnsmessage.TypeSRV
		err = b.Question(qt)
		if err != nil {
			return fmt.Errorf("question SRV: %v", err)
		}
		qt.Type = dnsmessage.TypeTXT
		if err = b.Question(qt); err != nil {
			return fmt.Errorf("question TXT: %v", err)
		}
	} else {
		q := dnsmessage.Question{
			Class: dnsmessage.ClassINET,
			Type:  dnsmessage.TypePTR,
			Name:  dnsmessage.MustNewName(serviceName),
		}
		if err = b.Question(q); err != nil {
			return fmt.Errorf("question PTR: %v", err)
		}
	}
	if buf, err = b.Finish(); err != nil {
		return err
	}
	if err := c.sendQuery(buf); err != nil {
		return err
	}
	return nil
}

// Pack the dns.Msg and write to available connections (multicast)
func (c *client) sendQuery(buf []byte) (err error) {
	for _, conn := range c.conns {
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
