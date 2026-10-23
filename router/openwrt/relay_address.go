package openwrt

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func (d *relayDNS) localAddress(ctx context.Context, network string) (string, error) {
	var status struct {
		Up     bool   `json:"up"`
		Device string `json:"l3_device"`
	}
	if err := d.call("network.interface."+network, "status", map[string]any{}, &status); err != nil {
		return "", err
	}
	if !status.Up || status.Device == "" {
		return "", errors.New("network interface is not up")
	}
	iface, err := net.InterfaceByName(status.Device)
	if err != nil {
		return "", err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}
	return waitRelayDNS(ctx, iface.Name, relayAddresses(addrs), probeRelayDNS)
}

// procd may return from restart before dnsmasq binds its sockets. Bound the
// entire readiness wait, including all addresses and both transports, and let
// service shutdown interrupt probes as well as the delay between attempts.
func waitRelayDNS(ctx context.Context, device string, addresses []netip.Addr, probe func(context.Context, string) error) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var failures []error
	for len(addresses) != 0 {
		failures = nil
		for _, ip := range addresses {
			if ctx.Err() != nil {
				break
			}
			target := ip
			if ip.IsLinkLocalUnicast() {
				target = ip.WithZone(device)
			}
			if err := probe(ctx, net.JoinHostPort(target.String(), "53")); err != nil {
				failures = append(failures, err)
				continue
			}
			if ctx.Err() == nil {
				// Zones select the local interface, but aren't advertised in DNS options.
				return ip.String(), nil
			}
		}
		if ctx.Err() != nil {
			break
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if ctx.Err() != nil {
			break
		}
	}
	return "", fmt.Errorf("no reachable local IPv6 DNS service on %s: %w", device,
		errors.Join(append(failures, ctx.Err(), errors.New("need a ULA or link-local address serving UDP and TCP port 53"))...))
}

func relayAddresses(addrs []net.Addr) []netip.Addr {
	var result []netip.Addr
	for _, addr := range addrs {
		p, err := netip.ParsePrefix(addr.String())
		if err != nil {
			continue
		}
		ip := p.Addr()
		// Avoid advertising an ISP address that can disappear on prefix renewal.
		if ip.Is6() && !ip.Is4In6() && (ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
			result = append(result, ip)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].IsPrivate() != result[j].IsPrivate() {
			return result[i].IsPrivate()
		}
		return result[i].Less(result[j])
	})
	return result
}

func relayAdvertisedDNS(original any, address string) []any {
	result := []any{}
	values, ok := original.([]any)
	if !ok {
		// UCI expands scalar array options on spaces and tabs, whereas list
		// entries are already individual values. Keep the original untouched
		// for exact restoration from the journal.
		if scalar, ok := original.(string); ok {
			for _, value := range strings.FieldsFunc(scalar, func(r rune) bool { return r == ' ' || r == '\t' }) {
				values = append(values, value)
			}
		}
	}
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if ip, err := netip.ParseAddr(s); err == nil && ip.Is4() {
			result = append(result, s)
		}
	}
	// Preserve explicit IPv4 advertisements; only replace the IPv6 resolvers.
	return append(result, address)
}

func probeRelayDNS(ctx context.Context, address string) error {
	var id [2]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	query := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: binary.BigEndian.Uint16(id[:]), RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("localhost."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
	}
	packet, err := query.Pack()
	if err != nil {
		return err
	}
	for _, network := range []string{"udp", "tcp"} {
		if err := probeRelayTransport(ctx, network, address, packet, query); err != nil {
			return fmt.Errorf("%s DNS at %s: %w", network, address, errors.Join(err, ctx.Err()))
		}
	}
	return nil
}

func probeRelayTransport(ctx context.Context, network, address string, packet []byte, query dnsmessage.Message) error {
	dialer := net.Dialer{Timeout: time.Second}
	c, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	deadline := time.Now().Add(time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := c.SetDeadline(deadline); err != nil {
		return err
	}
	var length [2]byte
	if network == "tcp" {
		binary.BigEndian.PutUint16(length[:], uint16(len(packet)))
		if _, err := c.Write(length[:]); err != nil {
			return err
		}
	}
	if _, err := c.Write(packet); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	var n int
	if network == "tcp" {
		if _, err := io.ReadFull(c, length[:]); err != nil {
			return err
		}
		n = int(binary.BigEndian.Uint16(length[:]))
		if n > len(buf) {
			return errors.New("oversized DNS probe response")
		}
		_, err = io.ReadFull(c, buf[:n])
	} else {
		n, err = c.Read(buf)
	}
	if err != nil {
		return err
	}
	var reply dnsmessage.Message
	if err := reply.Unpack(buf[:n]); err != nil {
		return err
	}
	if !reply.Response || reply.ID != query.ID || len(reply.Questions) != 1 || reply.Questions[0] != query.Questions[0] ||
		(reply.RCode != dnsmessage.RCodeSuccess && reply.RCode != dnsmessage.RCodeNameError) {
		return errors.New("invalid or unsuccessful DNS probe response")
	}
	return nil
}
