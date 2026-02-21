package resolver

import (
	"testing"
	"time"
)

// testDNSResponse is a wire-format DNS response for test.com. A IN
// with TTL 3600 and answer 69.172.200.235 (reused from cache_test.go).
var testDNSResponse = []byte{
	0xa6, 0xed, // ID
	0x81, 0x80, // Flags
	0x00, 0x01, // Questions
	0x00, 0x01, // Answers
	0x00, 0x00, // Authorities
	0x00, 0x01, // Additionals
	// Questions
	0x04, 0x74, 0x65, 0x73, 0x74, 0x03, 0x63, 0x6f, 0x6d, 0x00, // Label test.com.
	0x00, 0x01, // Type A
	0x00, 0x01, // Class IN
	// Answers
	0xc0, 0x0c, // Label pointer test.com.
	0x00, 0x01, // Type A
	0x00, 0x01, // Class IN
	0x00, 0x00, 0x0e, 0x10, // TTL 3600
	0x00, 0x04, // Data len 4
	0x45, 0xac, 0xc8, 0xeb, // 69.172.200.235
	// Additionals
	0x00,       // Label <root>
	0x00, 0x29, // Type OPT
	0x05, 0xac, // UDP packet size
	0x00,       // Extended RCODE
	0x00,       // EDNS Version
	0x00, 0x00, // Flags
	0x00, 0x00, // DATA
}

// testDNSResponseAAAA is a wire-format DNS response for test.com. AAAA IN
// with TTL 7200 and answer 2001:db8::1.
var testDNSResponseAAAA = []byte{
	0xb7, 0xfe, // ID
	0x81, 0x80, // Flags
	0x00, 0x01, // Questions
	0x00, 0x01, // Answers
	0x00, 0x00, // Authorities
	0x00, 0x00, // Additionals
	// Questions
	0x04, 0x74, 0x65, 0x73, 0x74, 0x03, 0x63, 0x6f, 0x6d, 0x00, // Label test.com.
	0x00, 0x1c, // Type AAAA
	0x00, 0x01, // Class IN
	// Answers
	0xc0, 0x0c, // Label pointer test.com.
	0x00, 0x1c, // Type AAAA
	0x00, 0x01, // Class IN
	0x00, 0x00, 0x1c, 0x20, // TTL 7200
	0x00, 0x10, // Data len 16
	0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // 2001:db8::1
}

func TestByteCache_Dump_Empty(t *testing.T) {
	bc, err := NewByteCache(1024*1024, false)
	if err != nil {
		t.Fatal(err)
	}
	entries := bc.Dump()
	if len(entries) != 0 {
		t.Fatalf("expected empty dump, got %d entries", len(entries))
	}
}

func TestByteCache_Dump(t *testing.T) {
	bc, err := NewByteCache(1024*1024, false)
	if err != nil {
		t.Fatal(err)
	}

	msg := make([]byte, len(testDNSResponse))
	copy(msg, testDNSResponse)

	v := &cacheValue{
		time:  time.Now(),
		msg:   msg,
		trans: "h2",
	}

	key := uint64(12345)
	bc.Set(key, v)
	bc.c.Wait() // wait for ristretto async set

	entries := bc.Dump()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Domain != "test.com." {
		t.Errorf("domain = %q, want %q", e.Domain, "test.com.")
	}
	if e.Type != "A" {
		t.Errorf("type = %q, want %q", e.Type, "A")
	}
	if e.Class != "INET" {
		t.Errorf("class = %q, want %q", e.Class, "INET")
	}
	if e.Transport != "h2" {
		t.Errorf("transport = %q, want %q", e.Transport, "h2")
	}
	if e.Size != len(testDNSResponse) {
		t.Errorf("size = %d, want %d", e.Size, len(testDNSResponse))
	}
	if e.TTL == 0 || e.TTL > 3600 {
		t.Errorf("ttl = %d, want > 0 and <= 3600", e.TTL)
	}
	if len(e.Answers) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(e.Answers))
	}
	a := e.Answers[0]
	if a.Data != "69.172.200.235" {
		t.Errorf("answer data = %q, want %q", a.Data, "69.172.200.235")
	}
	if a.Type != "A" {
		t.Errorf("answer type = %q, want %q", a.Type, "A")
	}
}

func TestByteCache_Dump_SetTracked(t *testing.T) {
	bc, err := NewByteCache(1024*1024, false)
	if err != nil {
		t.Fatal(err)
	}

	msg := make([]byte, len(testDNSResponse))
	copy(msg, testDNSResponse)

	v := &cacheValue{
		time:  time.Now(),
		msg:   msg,
		trans: "h2",
	}

	key := uint64(99999)
	bc.SetTracked(key, v, "https://dns.nextdns.io/abc123")
	bc.c.Wait()

	entries := bc.Dump()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Key == "" {
		t.Error("expected non-empty key string")
	}
	// Key should contain the context URL
	want := "https://dns.nextdns.io/abc123 INET A test.com."
	if e.Key != want {
		t.Errorf("key = %q, want %q", e.Key, want)
	}
}

func TestByteCache_Dump_MultipleEntries(t *testing.T) {
	bc, err := NewByteCache(1024*1024, false)
	if err != nil {
		t.Fatal(err)
	}

	// Set A record
	msgA := make([]byte, len(testDNSResponse))
	copy(msgA, testDNSResponse)
	vA := &cacheValue{
		time:  time.Now(),
		msg:   msgA,
		trans: "h2",
	}
	bc.Set(uint64(1), vA)

	// Set AAAA record
	msgAAAA := make([]byte, len(testDNSResponseAAAA))
	copy(msgAAAA, testDNSResponseAAAA)
	vAAAA := &cacheValue{
		time:  time.Now(),
		msg:   msgAAAA,
		trans: "h3",
	}
	bc.Set(uint64(2), vAAAA)

	bc.c.Wait()

	entries := bc.Dump()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Find each entry by type
	var foundA, foundAAAA bool
	for _, e := range entries {
		switch e.Type {
		case "A":
			foundA = true
			if len(e.Answers) != 1 || e.Answers[0].Data != "69.172.200.235" {
				t.Errorf("A answer mismatch: %+v", e.Answers)
			}
		case "AAAA":
			foundAAAA = true
			if len(e.Answers) != 1 || e.Answers[0].Data != "2001:db8::1" {
				t.Errorf("AAAA answer mismatch: %+v", e.Answers)
			}
			if e.Transport != "h3" {
				t.Errorf("transport = %q, want %q", e.Transport, "h3")
			}
		}
	}
	if !foundA {
		t.Error("A record not found in dump")
	}
	if !foundAAAA {
		t.Error("AAAA record not found in dump")
	}
}

func TestByteCache_Metrics(t *testing.T) {
	bc, err := NewByteCache(1024*1024, true)
	if err != nil {
		t.Fatal(err)
	}
	m := bc.Metrics()
	if m == nil {
		t.Fatal("expected non-nil metrics when enabled")
	}

	bcNoMetrics, err := NewByteCache(1024*1024, false)
	if err != nil {
		t.Fatal(err)
	}
	if bcNoMetrics.Metrics() != nil {
		t.Fatal("expected nil metrics when disabled")
	}
}
