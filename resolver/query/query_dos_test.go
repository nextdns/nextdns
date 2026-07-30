package query

import (
	"encoding/hex"
	"net"
	"testing"
	"time"
)

// TestNewSkipsNonOPTThenReadsOPT covers the functional half of the fix: a
// non-OPT additional record before the OPT must be skipped, and the OPT must
// still be found and its ECS peer IP extracted. Guards against a future change
// that skips the record but also loses the OPT.
func TestNewSkipsNonOPTThenReadsOPT(t *testing.T) {
	msg := []byte{
		0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, // hdr qd=1 ar=2
		0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01, // question a. A IN
		0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x09, 0x09, 0x09, 0x09, // additional 1: A record
		0x00, 0x00, 0x29, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0c, // additional 2: OPT, rdlen 12
		0x00, 0x08, 0x00, 0x08, 0x00, 0x01, 0x20, 0x00, 0x01, 0x02, 0x03, 0x04, // ECS /32 = 1.2.3.4
	}
	q, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !q.PeerIP.Equal(net.IPv4(1, 2, 3, 4)) {
		t.Errorf("PeerIP = %v, want 1.2.3.4 (ECS after a skipped non-OPT record)", q.PeerIP)
	}
}

// TestNewReturnsOnNonOPTAdditional is a regression for an infinite loop: a
// well-formed query carrying a non-OPT additional record (here a single A
// record) spun query.New forever, because the additional-section loop re-read a
// non-OPT header without consuming the record. It is reachable from one UDP
// datagram, so a single packet pinned a core.
//
// The check is timeout-guarded because a regression hangs rather than fails; the
// goroutine is then leaked until the test process exits, which is acceptable for
// a one-shot regression.
func TestNewReturnsOnNonOPTAdditional(t *testing.T) {
	msg, err := hex.DecodeString(
		"123401000001000000000001016100000100010161000001000100000000000401020304")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("query.New did not return within 2s: infinite loop on a non-OPT additional record")
	}
}
