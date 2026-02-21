package endpoint

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestDNSEndpointExchange_MatchesResponseID(t *testing.T) {
	ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		n, raddr, err := ln.ReadFromUDP(buf)
		if err != nil || n < 2 {
			return
		}
		// Echo a minimal response with the same ID.
		resp := make([]byte, 12)
		resp[0], resp[1] = buf[0], buf[1]
		_, _ = ln.WriteToUDP(resp, raddr)
	}()

	e := &DNSEndpoint{Addr: ln.LocalAddr().String()}
	payload := make([]byte, 12)
	buf := make([]byte, 514)
	buf[1] = 0xFF // would have broken the old buggy ID calculation.

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	n, err := e.Exchange(ctx, payload, buf)
	if err != nil {
		t.Fatalf("Exchange error: %v", err)
	}
	if n < 2 {
		t.Fatalf("expected n>=2, got %d", n)
	}

	<-done
}

