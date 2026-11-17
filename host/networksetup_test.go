//go:build unix

package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubNetworkSetup writes a fake networksetup listing Wi-Fi, Ethernet, VPN and
// a disabled service. Applied changes are appended to the returned log file.
// behavior maps a service name to a shell snippet run before the change is
// applied, e.g. a failure or a hang.
func stubNetworkSetup(t *testing.T, behavior map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	var cases strings.Builder
	for svc, sh := range behavior {
		cases.WriteString("\t\"" + svc + "\") " + sh + " ;;\n")
	}
	script := `#!/bin/sh
if [ "$1" = -listallnetworkservices ]; then
	printf 'An asterisk (*) denotes that a network service is disabled.\nWi-Fi\nEthernet\nVPN\n*Bluetooth PAN\n'
	exit 0
fi
case "$2" in
` + cases.String() + `esac
echo "$2=$3" >> "` + log + `"
`
	path := filepath.Join(dir, "networksetup")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, log
}

func readApplied(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(b))
}

func setDNSWithin(t *testing.T, n networkSetup, dns string, d time.Duration) error {
	t.Helper()
	errC := make(chan error, 1)
	go func() { errC <- n.setDNS(dns) }()
	select {
	case err := <-errC:
		return err
	case <-time.After(d):
		t.Fatalf("setDNS(%q) did not return within %v", dns, d)
		return nil
	}
}

func TestNetworkSetupSetDNS(t *testing.T) {
	path, log := stubNetworkSetup(t, nil)
	n := networkSetup{path: path, timeout: 5 * time.Second}
	if err := setDNSWithin(t, n, "127.0.0.1", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(readApplied(t, log), " ")
	if want := "Wi-Fi=127.0.0.1 Ethernet=127.0.0.1 VPN=127.0.0.1"; got != want {
		t.Fatalf("applied %q, want %q", got, want)
	}
}

// One service succeeds while another (here the active one) fails: SetDNS must
// report the failure and still configure the other services.
func TestNetworkSetupSetDNSPartialFailure(t *testing.T) {
	for _, failing := range []string{"Wi-Fi", "Ethernet"} {
		t.Run(failing, func(t *testing.T) {
			path, log := stubNetworkSetup(t, map[string]string{
				failing: `echo "** Error: The parameters were not valid."; exit 4`,
			})
			n := networkSetup{path: path, timeout: 5 * time.Second}
			err := setDNSWithin(t, n, "127.0.0.1", 10*time.Second)
			if err == nil {
				t.Fatal("setDNS returned nil with a failing service")
			}
			if !strings.Contains(err.Error(), "-setdnsservers "+failing+" 127.0.0.1: ** Error: The parameters were not valid.") {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, a := range readApplied(t, log) {
				if strings.HasPrefix(a, failing+"=") {
					t.Fatalf("failing service applied: %v", a)
				}
			}
			if got := len(readApplied(t, log)); got != 2 {
				t.Fatalf("applied %v, want the 2 other services", readApplied(t, log))
			}
		})
	}
}

// A stalled networksetup (whose child keeps stdout open) must be killed after
// the timeout and reported, without blocking the other services.
func TestNetworkSetupSetDNSHang(t *testing.T) {
	path, log := stubNetworkSetup(t, map[string]string{"Wi-Fi": "sleep 30"})
	n := networkSetup{path: path, timeout: 200 * time.Millisecond}
	err := setDNSWithin(t, n, "127.0.0.1", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "-setdnsservers Wi-Fi 127.0.0.1: timed out after 200ms") {
		t.Fatalf("want timeout error, got %v", err)
	}
	got := strings.Join(readApplied(t, log), " ")
	if want := "Ethernet=127.0.0.1 VPN=127.0.0.1"; got != want {
		t.Fatalf("applied %q, want %q", got, want)
	}
}

func TestNetworkSetupListHang(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "networksetup")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	n := networkSetup{path: path, timeout: 200 * time.Millisecond}
	err := setDNSWithin(t, n, "127.0.0.1", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "-listallnetworkservices: timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
}

// Activation that timed out must not change DNS later: once setDNS has
// returned and deactivation reset DNS, the stalled call must never apply.
func TestNetworkSetupNoChangeAfterDeactivate(t *testing.T) {
	path, log := stubNetworkSetup(t, map[string]string{"VPN": `[ "$3" = empty ] || sleep 1`})
	n := networkSetup{path: path, timeout: 200 * time.Millisecond}
	if err := setDNSWithin(t, n, "127.0.0.1", 5*time.Second); err == nil {
		t.Fatal("want timeout error from activation")
	}
	if err := setDNSWithin(t, n, "empty", 5*time.Second); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	time.Sleep(1500 * time.Millisecond) // past the stalled call's sleep
	got := strings.Join(readApplied(t, log), " ")
	if want := "Wi-Fi=127.0.0.1 Ethernet=127.0.0.1 Wi-Fi=empty Ethernet=empty VPN=empty"; got != want {
		t.Fatalf("applied %q, want %q", got, want)
	}
}
