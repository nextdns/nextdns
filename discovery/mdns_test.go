package discovery

import (
	"fmt"
	"testing"
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
