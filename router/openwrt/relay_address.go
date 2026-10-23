package openwrt

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func (d *relayDNS) localAddress(network string) (string, error) {
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
	var failures []error
	for _, ip := range relayAddresses(addrs) {
		target := ip
		if ip.IsLinkLocalUnicast() {
			target = ip.WithZone(iface.Name)
		}
		if err := probeRelayDNS(net.JoinHostPort(target.String(), "53")); err != nil {
			failures = append(failures, err)
			continue
		}
		// A zone identifies the outgoing interface locally, but isn't part of an
		// address in an RDNSS/DHCPv6 option (nor a valid odhcpd DNS configuration).
		return ip.String(), nil
	}
	return "", fmt.Errorf("no reachable local IPv6 DNS service on %s: %w", iface.Name,
		errors.Join(append([]error{errors.New("need a ULA or link-local address serving UDP and TCP port 53")}, failures...)...))
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
	if !ok && original != nil {
		values = []any{original}
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

func probeRelayDNS(address string) error {
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
		if err := probeRelayTransport(network, address, packet, query); err != nil {
			return fmt.Errorf("%s DNS at %s: %w", network, address, err)
		}
	}
	return nil
}

func probeRelayTransport(network, address string, packet []byte, query dnsmessage.Message) error {
	c, err := net.DialTimeout(network, address, time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(time.Second)); err != nil {
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
