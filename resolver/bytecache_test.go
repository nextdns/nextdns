package resolver

import (
	"reflect"
	"testing"

	"github.com/nextdns/nextdns/resolver/query"
)

func TestByteCacheKeys(t *testing.T) {
	bc, err := NewByteCache(1024, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := bc.Keys(); len(got) != 0 {
		t.Fatalf("Keys() = %v, want empty", got)
	}

	k1 := cacheKey{ctx: "https://dns.example/a", qclass: query.ClassINET, qtype: query.TypeA, qname: "one.example."}
	k2 := cacheKey{ctx: "https://dns.example/a", qclass: query.ClassINET, qtype: query.TypeA, qname: "two.example."}
	bc.Set(k1.Hash(), &cacheValue{msg: []byte{1}, label: k1.String()})
	bc.Set(k2.Hash(), &cacheValue{msg: []byte{2}, label: k2.String()})

	got := bc.Keys()
	want := []string{k1.String(), k2.String()}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Keys() = %v, want %v", got, want)
	}
}
