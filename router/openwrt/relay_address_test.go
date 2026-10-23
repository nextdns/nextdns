package openwrt

import (
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestRelayAddresses(t *testing.T) {
	var addrs []net.Addr
	for _, s := range []string{"2001:db8::1/64", "fe80::2/64", "127.0.0.1/8", "::1/128", "fd00::2/64", "fd00::1/64", "::ffff:192.168.1.1/128"} {
		ip, n, _ := net.ParseCIDR(s)
		n.IP = ip
		addrs = append(addrs, n)
	}
	want := []netip.Addr{netip.MustParseAddr("fd00::1"), netip.MustParseAddr("fd00::2"), netip.MustParseAddr("fe80::2")}
	if got := relayAddresses(addrs); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v != %v", got, want)
	}
}

func TestProbeRelayDNS(t *testing.T) {
	for _, mode := range []string{"success", "nxdomain", "wrong id", "wrong question", "refused", "malformed", "tcp unavailable", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			udp, err := net.ListenPacket("udp6", "[::1]:0")
			if err != nil {
				t.Fatal(err)
			}
			defer udp.Close()
			tcp, err := net.Listen("tcp6", udp.LocalAddr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer tcp.Close()
			respond := func(b []byte) []byte {
				var m dnsmessage.Message
				if err := m.Unpack(b); err != nil {
					return nil
				}
				m.Response = true
				switch mode {
				case "nxdomain":
					m.RCode = dnsmessage.RCodeNameError
				case "wrong id":
					m.ID++
				case "wrong question":
					m.Questions[0].Name = dnsmessage.MustNewName("wrong.")
				case "refused":
					m.RCode = dnsmessage.RCodeRefused
				case "malformed":
					return []byte{0}
				}
				out, _ := m.Pack()
				return out
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				buf := make([]byte, 4096)
				n, addr, err := udp.ReadFrom(buf)
				if err != nil {
					return
				}
				if mode != "timeout" {
					_, _ = udp.WriteTo(respond(buf[:n]), addr)
				}
			}()
			tcpDone := make(chan struct{})
			go func() {
				defer close(tcpDone)
				c, err := tcp.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				var length [2]byte
				if _, err := io.ReadFull(c, length[:]); err != nil {
					return
				}
				b := make([]byte, int(binary.BigEndian.Uint16(length[:])))
				if _, err := io.ReadFull(c, b); err != nil {
					return
				}
				out := respond(b)
				binary.BigEndian.PutUint16(length[:], uint16(len(out)))
				_, _ = c.Write(append(length[:], out...))
			}()
			if mode == "tcp unavailable" {
				_ = tcp.Close()
			}
			err = probeRelayDNS(udp.LocalAddr().String())
			success := mode == "success" || mode == "nxdomain"
			if (err == nil) != success {
				t.Errorf("probe = %v; want success %v", err, success)
			}
			_ = udp.Close()
			_ = tcp.Close()
			<-done
			<-tcpDone
		})
	}
}
