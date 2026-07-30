package query

import (
	"bytes"
	"net"
	"testing"
)

// buildQuery hand-crafts a DNS query on the wire so the tests do not depend on
// the message package they exist to guard. opts are raw EDNS options
// (code+len+value already encoded) placed in an OPT record; nil means no OPT.
func buildQuery(id uint16, qname string, qtype Type, opts []byte) []byte {
	msg := []byte{byte(id >> 8), byte(id), 0x01, 0x00} // id, RD set
	arcount := 0
	if opts != nil {
		arcount = 1
	}
	msg = append(msg, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, byte(arcount>>8), byte(arcount))
	for _, label := range splitName(qname) {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0x00)                        // root
	msg = append(msg, byte(qtype>>8), byte(qtype)) // type
	msg = append(msg, 0x00, 0x01)                  // class IN
	if opts != nil {
		msg = append(msg, 0x00)                   // OPT name = root
		msg = append(msg, 0x00, 0x29)             // type OPT
		msg = append(msg, 0x10, 0x00)             // UDP size 4096
		msg = append(msg, 0x00, 0x00, 0x00, 0x00) // extended rcode + flags
		msg = append(msg, byte(len(opts)>>8), byte(len(opts)))
		msg = append(msg, opts...)
	}
	return msg
}

func splitName(name string) [][]byte {
	if name == "" {
		return nil
	}
	return bytes.Split([]byte(name), []byte("."))
}

// ecsOption encodes an EDNS Client Subnet option: family, source prefix, scope
// 0, then the address bytes.
func ecsOption(family uint16, sourcePrefix byte, addr []byte) []byte {
	data := []byte{byte(family >> 8), byte(family), sourcePrefix, 0x00}
	data = append(data, addr...)
	opt := []byte{0x00, 0x08, byte(len(data) >> 8), byte(len(data))}
	return append(opt, data...)
}

func TestNewParsesQuestion(t *testing.T) {
	msg := buildQuery(0xbeef, "example.com", TypeA, nil)
	q, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if q.ID != 0xbeef {
		t.Errorf("ID = %#x, want 0xbeef", q.ID)
	}
	if q.Name != "example.com." {
		t.Errorf("Name = %q, want %q", q.Name, "example.com.")
	}
	if q.Type != TypeA {
		t.Errorf("Type = %v, want A", q.Type)
	}
	if q.Class != ClassINET {
		t.Errorf("Class = %v, want INET", q.Class)
	}
	if !q.RecursionDesired {
		t.Error("RecursionDesired = false, want true")
	}
}

func TestNewExtractsECSPeerIP(t *testing.T) {
	for _, tt := range []struct {
		name   string
		family uint16
		prefix byte
		addr   []byte
		want   net.IP
	}{
		{"ipv4 /32", 0x1, 32, []byte{203, 0, 113, 7}, net.IP{203, 0, 113, 7}},
		{
			"ipv6 /128", 0x2, 128,
			[]byte{0x20, 0x01, 0xd, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			net.IP{0x20, 0x01, 0xd, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msg := buildQuery(1, "a.test", TypeA, ecsOption(tt.family, tt.prefix, tt.addr))
			q, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if !q.PeerIP.Equal(tt.want) {
				t.Errorf("PeerIP = %v, want %v", q.PeerIP, tt.want)
			}
		})
	}
}

// TestNewNeutersECS is the behaviour the DataOffset field exists to serve: the
// ECS option must be zeroed and its code set to 0xFFFF in the outgoing payload,
// so the client subnet never reaches the upstream. This is the assertion the
// de-fork must not change.
func TestNewNeutersECS(t *testing.T) {
	ecs := ecsOption(0x1, 32, []byte{203, 0, 113, 7})
	msg := buildQuery(1, "a.test", TypeA, ecs)

	// Locate the option in the payload before New mutates it.
	optStart := bytes.Index(msg, ecs)
	if optStart < 0 {
		t.Fatal("could not locate ECS option in crafted payload")
	}

	if _, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1)); err != nil {
		t.Fatalf("New: %v", err)
	}

	// Code neutered to 0xFFFF.
	if msg[optStart] != 0xFF || msg[optStart+1] != 0xFF {
		t.Errorf("option code = %#x %#x, want 0xFF 0xFF", msg[optStart], msg[optStart+1])
	}
	// Every data byte zeroed (option header is 4 bytes: code+len).
	data := msg[optStart+4 : optStart+4+len(ecs)-4]
	if !bytes.Equal(data, make([]byte, len(data))) {
		t.Errorf("ECS data not zeroed: %v", data)
	}
}

// TestNewNeutersOPTNotName guards the offset re-derivation: a query whose name
// embeds the exact bytes of its own ECS option must still neuter the real
// option in the OPT record, not the lookalike bytes in the name. This is the
// case a first-match search gets wrong.
func TestNewNeutersOPTNotName(t *testing.T) {
	ecs := ecsOption(0x1, 32, []byte{0xCA, 0xCA, 0xCA, 0xCA}) // 12 bytes

	msg := []byte{0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0x00, 0x01} // hdr qd=1 ar=1
	nameOff := len(msg)
	msg = append(msg, byte(len(ecs))) // one label = the ECS bytes
	msg = append(msg, ecs...)
	msg = append(msg, 0x00, 0x00, 0x01, 0x00, 0x01) // root, type A, class IN
	optDataOff := len(msg) + 11 + 4                 // OPT preamble(11) + option header(4)
	msg = append(msg, 0x00, 0x00, 0x29, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, byte(len(ecs)>>8), byte(len(ecs)))
	msg = append(msg, ecs...)

	q, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !q.PeerIP.Equal(net.IP{0xCA, 0xCA, 0xCA, 0xCA}) {
		t.Errorf("PeerIP = %v, want 202.202.202.202", q.PeerIP)
	}
	// The name copy must be untouched: still the original ECS bytes.
	if !bytes.Equal(msg[nameOff+1:nameOff+1+len(ecs)], ecs) {
		t.Errorf("name bytes were altered: %v", msg[nameOff+1:nameOff+1+len(ecs)])
	}
	// The real OPT option data must be zeroed.
	data := msg[optDataOff : optDataOff+len(ecs)-4]
	if !bytes.Equal(data, make([]byte, len(data))) {
		t.Errorf("OPT ECS data not zeroed: %v", data)
	}
}

// TestNewNeutersOPTNotTrailingDecoy guards the other half of the offset
// re-derivation: a copy of the ECS option placed after the OPT record (here in
// a second additional RR the parse loop never reaches) must not divert the
// neutering. The real option must be zeroed and the client subnet must not
// survive into the payload.
func TestNewNeutersOPTNotTrailingDecoy(t *testing.T) {
	ecs := ecsOption(0x1, 32, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	msg := []byte{0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0x00, 0x02} // hdr qd=1 ar=2
	msg = append(msg, 0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01)                // name "a", A, IN
	optDataOff := len(msg) + 11 + 4
	msg = append(msg, 0x00, 0x00, 0x29, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, byte(len(ecs)>>8), byte(len(ecs)))
	msg = append(msg, ecs...)
	// second additional RR (TXT) carrying a decoy copy of the option
	decoyOff := len(msg) + 11
	msg = append(msg, 0x00, 0x00, 0x10, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, byte(len(ecs)>>8), byte(len(ecs)))
	msg = append(msg, ecs...)

	if _, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1)); err != nil {
		t.Fatalf("New: %v", err)
	}
	// Real OPT option data zeroed.
	if got := msg[optDataOff : optDataOff+len(ecs)-4]; !bytes.Equal(got, make([]byte, len(got))) {
		t.Errorf("real OPT ECS not zeroed: %v", got)
	}
	// Decoy untouched (proves neutering stayed inside the OPT rdata).
	if got := msg[decoyOff : decoyOff+len(ecs)]; !bytes.Equal(got, ecs) {
		t.Errorf("decoy was altered: %v", got)
	}
}

// TestNewNeutersUnderstatedRdlen guards that an OPT rdlen understating the ECS
// option -- which the parser reads in full anyway -- does not defeat the
// stripping. A bounded search misses the option and leaks; lockstep offsets do
// not depend on rdlen.
func TestNewNeutersUnderstatedRdlen(t *testing.T) {
	ecs := ecsOption(0x1, 32, []byte{0x0A, 0x0B, 0x0C, 0x0D}) // 12 bytes on the wire

	msg := []byte{0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0x00, 0x01} // hdr qd=1 ar=1
	msg = append(msg, 0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01)                // name "a", A, IN
	optDataOff := len(msg) + 11 + 4
	// rdlen deliberately understated to 8 while the option is 12 bytes.
	msg = append(msg, 0x00, 0x00, 0x29, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08)
	msg = append(msg, ecs...)

	if _, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1)); err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := msg[optDataOff : optDataOff+len(ecs)-4]; !bytes.Equal(got, make([]byte, len(got))) {
		t.Errorf("ECS data not zeroed under understated rdlen (leak): %v", got)
	}
}

// TestNewNeutersECSAfterAnotherOption exercises the lockstep advance: with a MAC
// option before the ECS option, the offset must step past the MAC to land on the
// ECS. A broken advance points at the wrong option and the code-match guard then
// skips neutering, leaking ECS.
func TestNewNeutersECSAfterAnotherOption(t *testing.T) {
	mac := []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}
	macOpt := append([]byte{0xfd, 0xe9, 0x00, byte(len(mac))}, mac...)
	ecs := ecsOption(0x1, 32, []byte{0x0A, 0x0B, 0x0C, 0x0D})

	msg := buildQuery(1, "a.test", TypeA, append(macOpt, ecs...))
	ecsDataOff := bytes.Index(msg, ecs) + 4

	q, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !bytes.Equal(q.MAC, mac) {
		t.Errorf("MAC = %v, want %v", q.MAC, mac)
	}
	if got := msg[ecsDataOff : ecsDataOff+len(ecs)-4]; !bytes.Equal(got, make([]byte, len(got))) {
		t.Errorf("ECS after MAC option not zeroed (advance broken): %v", got)
	}
}

// FuzzOptRDataStart asserts the untrusted-input walker never panics and always
// returns an in-range offset. It does not call New: an unrelated pre-existing
// hang on a non-OPT additional record would trap the fuzzer.
func FuzzOptRDataStart(f *testing.F) {
	f.Add([]byte{})
	f.Add(buildQuery(1, "a.b", TypeA, ecsOption(0x1, 32, []byte{1, 2, 3, 4})))
	f.Fuzz(func(t *testing.T, msg []byte) {
		if off := optRDataStart(msg); off < -1 || off > len(msg) {
			t.Fatalf("optRDataStart returned %d out of range for len %d", off, len(msg))
		}
	})
}

func TestNewExtractsMAC(t *testing.T) {
	mac := []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}
	// EDNS0_MAC option, code 0xfde9.
	opt := []byte{0xfd, 0xe9, 0x00, byte(len(mac))}
	opt = append(opt, mac...)
	msg := buildQuery(1, "a.test", TypeA, opt)
	q, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !bytes.Equal(q.MAC, mac) {
		t.Errorf("MAC = %v, want %v", q.MAC, mac)
	}
}

func TestNewNoOPT(t *testing.T) {
	msg := buildQuery(1, "a.test", TypeA, nil)
	q, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if q.Name != "a.test." {
		t.Errorf("Name = %q, want a.test.", q.Name)
	}
}

func TestNewIgnoresPartialECS(t *testing.T) {
	// Source prefix < 32 means the client sent a subnet, not a full IP; PeerIP
	// must not be taken from it.
	msg := buildQuery(1, "a.test", TypeA, ecsOption(0x1, 24, []byte{203, 0, 113, 0}))
	q, err := New(msg, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !q.PeerIP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("PeerIP = %v, want unchanged 127.0.0.1", q.PeerIP)
	}
}
