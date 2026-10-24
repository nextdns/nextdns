package openwrt

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"syscall"
	"testing"
	"testing/synctest"
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
			err = probeRelayDNS(context.Background(), udp.LocalAddr().String())
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

func TestRelayAdvertisedDNSScalar(t *testing.T) {
	for _, original := range []string{
		"192.168.2.1 2001:db8::53", "192.168.2.1\t2001:db8::53",
		" \t192.168.2.1  2001:db8::53\t ",
	} {
		if got, want := relayAdvertisedDNS(original, "fd00::1"), []any{"192.168.2.1", "fd00::1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %#v, want %#v", original, got, want)
		}
	}
	// UCI doesn't split whitespace inside individual list entries.
	if got := relayAdvertisedDNS([]any{"192.168.2.1 2001:db8::53"}, "fd00::1"); !reflect.DeepEqual(got, []any{"fd00::1"}) {
		t.Fatalf("unexpected list parsing: %#v", got)
	}
}

func TestWaitRelayDNS(t *testing.T) {
	addresses := []netip.Addr{netip.MustParseAddr("fd00::1"), netip.MustParseAddr("fe80::1")}
	t.Run("delayed listener", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			started := time.Now()
			calls := 0
			ip, err := waitRelayDNS(context.Background(), "br-lan", addresses, func(_ context.Context, target string) error {
				calls++
				want := "[fd00::1]:53"
				if calls%2 == 0 {
					want = "[fe80::1%br-lan]:53"
				}
				if target != want {
					t.Fatalf("target = %q, want %q", target, want)
				}
				if time.Since(started) < time.Second {
					return syscall.ECONNREFUSED
				}
				return nil
			})
			if err != nil || ip != "fd00::1" || calls != 9 {
				t.Fatalf("ip=%q calls=%d err=%v", ip, calls, err)
			}
		})
	})
	t.Run("unavailable", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			started := time.Now()
			_, err := waitRelayDNS(context.Background(), "br-lan", addresses, func(context.Context, string) error { return syscall.ECONNREFUSED })
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, syscall.ECONNREFUSED) || time.Since(started) != 5*time.Second {
				t.Fatalf("duration=%v err=%v", time.Since(started), err)
			}
		})
	})
	t.Run("cancel during retry delay", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			calls := 0
			go func() {
				_, err := waitRelayDNS(ctx, "br-lan", addresses, func(context.Context, string) error { calls++; return syscall.ECONNREFUSED })
				done <- err
			}()
			synctest.Wait()
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) || calls != 2 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	})
	t.Run("cancel during probe", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := waitRelayDNS(ctx, "br-lan", addresses, func(ctx context.Context, _ string) error { <-ctx.Done(); return ctx.Err() })
				done <- err
			}()
			synctest.Wait()
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	})
}

func TestProbeRelayDNSCancellation(t *testing.T) {
	udp, err := net.ListenPacket("udp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- probeRelayDNS(ctx, udp.LocalAddr().String()) }()
	// Wait for the query, then cancel while the probe is blocked reading its reply.
	if err := udp.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := udp.ReadFrom(make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("probe error = %v", err)
	}
}
