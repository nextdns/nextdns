package discovery

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkMDNS_removeOldestEntry(b *testing.B) {
	r := &MDNS{
		addrs: map[string]mdnsEntry{},
		names: map[string]mdnsEntry{},
	}

	addr := "10.0.0.1"
	namePrefix := "homeassistant"

	for i := range mdnsMaxEntries {
		name := fmt.Sprintf("%s%d", namePrefix, i)
		addEntry(r.addrs, addr, name)
		addEntry(r.names, name, addr)
	}
	// pre-alloc names
	names := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		names[i] = fmt.Sprintf("%s%d", namePrefix, mdnsMaxEntries+i)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		name := names[i]
		addEntry(r.addrs, addr, name)
		addEntry(r.names, name, addr)
		for len(r.names) > mdnsMaxEntries {
			r.removeOldestEntry()
		}
	}
}

func TestMDNS_removeOldestEntry_ConsistentReverseIndex(t *testing.T) {
	now := time.Now()
	r := &MDNS{
		addrs: map[string]mdnsEntry{
			"10.0.0.1": {values: []string{"host1.local.", "host2.local."}, lastUpdate: now},
			"10.0.0.2": {values: []string{"host1.local."}, lastUpdate: now},
		},
		names: map[string]mdnsEntry{
			"host1.local.": {values: []string{"10.0.0.1", "10.0.0.2"}, lastUpdate: now.Add(-time.Minute)},
			"host2.local.": {values: []string{"10.0.0.1"}, lastUpdate: now},
		},
	}

	r.removeOldestEntry()

	if _, ok := r.names["host1.local."]; ok {
		t.Fatalf("oldest name was not removed from names map")
	}
	if got := r.addrs["10.0.0.1"].values; len(got) != 1 || got[0] != "host2.local." {
		t.Fatalf("unexpected addrs entry for 10.0.0.1: %v", got)
	}
	if _, ok := r.addrs["10.0.0.2"]; ok {
		t.Fatalf("addr with only removed name should be deleted")
	}
}
