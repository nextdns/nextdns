package openwrt

import (
	"os"
	"reflect"
	"testing"
)

// Run only inside the disposable OpenWrt container created by
// testdata/relay-integration.sh. This test intentionally replaces /etc/config/dhcp.
func TestRelayOpenWrtUCI(t *testing.T) {
	if os.Getenv("NEXTDNS_TEST_OPENWRT_RELAY") != "1" {
		t.Skip("requires disposable OpenWrt container")
	}
	if _, err := os.Stat("/tmp/nextdns-relay-test-container"); err != nil {
		t.Fatal("missing container marker")
	}
	fixture := `config dnsmasq 'dnsmasq'
 list interface 'lan'
 list interface 'guest'
config dhcp 'lan'
 option interface 'lan'
 option ra 'relay'
 option dhcpv6 'relay'
config dhcp 'guest'
 option interface 'guest'
 option ra 'hybrid'
 list dns '192.168.2.1'
 list dns '2001:db8::53'
 list dns '2001:db8::54'
config dhcp 'scalar'
 option interface 'scalar'
 option dhcpv6 'relay'
 option dns '2001:db8::53'
config dhcp 'wan6'
 option master '1'
 option ra 'relay'
 list dns '2001:db8::53'
config dhcp
 option interface 'anonymous'
 option ra 'relay'
`
	if err := os.WriteFile("/etc/config/dhcp", []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	d := &relayDNS{
		statePath: relayStatePath, call: relayUbus, needed: relayNeeded,
		address: func(string) (string, error) { return "fd00::1", nil },
		// Test UCI/rpcd behavior here. Advertisement reload and address probing are
		// covered separately; the container does not run procd or a routed LAN.
		reload: func() error { return nil },
	}
	before, err := d.sections()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.setup(); err != nil {
		t.Fatal(err)
	}
	after, err := d.sections()
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range before {
		want := s.DNS
		if s.downstreamRelay() {
			want = relayAdvertisedDNS(s.DNS, "fd00::1")
		}
		if !reflect.DeepEqual(after[name].DNS, want) {
			t.Fatalf("%s DNS = %#v; want %#v", name, after[name].DNS, want)
		}
	}
	// Re-open the state through a fresh manager and restore exact scalar/list types.
	restarted := *d
	if err := restarted.restore(); err != nil {
		t.Fatal(err)
	}
	restored, err := d.sections()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, before) {
		t.Fatalf("restored %#v; want %#v", restored, before)
	}
	assertNoJournal(t, d)
	// Ensure a subsequent administrator list replacement is retained.
	if err := d.setup(); err != nil {
		t.Fatal(err)
	}
	if err := d.setDNS("guest", []any{"fd00::99"}); err != nil {
		t.Fatal(err)
	}
	if err := d.commit(); err != nil {
		t.Fatal(err)
	}
	if err := d.restore(); err != nil {
		t.Fatal(err)
	}
	restored, err = d.sections()
	if err != nil {
		t.Fatal(err)
	}
	s := before["guest"]
	s.DNS = []any{"fd00::99"}
	before["guest"] = s
	if !reflect.DeepEqual(restored, before) {
		t.Fatalf("admin change lost: %#v", restored)
	}
}
