//go:build linux

package systemd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

// stubArg makes the test binary act as a fake daemon when systemd starts it
// through the generated unit (see runStub).
const stubArg = "__nextdns_systemd_lifecycle_stub__"

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == stubArg {
		runStub(os.Args[2])
		return
	}
	os.Exit(m.Run())
}

// runStub records each start in dir/starts, then behaves according to the
// control files in dir:
//   - dir/crashloop present: exit 2 right away (persistent crash);
//   - dir/crash created later: exit 2 (like a Go runtime fatal error);
//   - dir/exit0 created later: exit 0 (clean exit);
//   - otherwise keep running until signaled.
func runStub(dir string) {
	f, err := os.OpenFile(filepath.Join(dir, "starts"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = fmt.Fprintln(f, os.Getpid())
		f.Close()
	}
	if _, err := os.Stat(filepath.Join(dir, "crashloop")); err == nil {
		os.Exit(2)
	}
	for {
		if os.Remove(filepath.Join(dir, "crash")) == nil {
			os.Exit(2)
		}
		if os.Remove(filepath.Join(dir, "exit0")) == nil {
			os.Exit(0)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestLifecycle installs the generated unit on a real systemd host and checks
// the restart behavior. It modifies the system, so it only runs when
// NEXTDNS_SYSTEMD_INTEGRATION=1 and as root under systemd.
func TestLifecycle(t *testing.T) {
	if os.Getenv("NEXTDNS_SYSTEMD_INTEGRATION") != "1" {
		t.Skip("set NEXTDNS_SYSTEMD_INTEGRATION=1 to run against the host systemd")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("nextdns-lifecycle-test-%d", os.Getpid())
	unit := name + ".service"
	s, err := New(service.Config{
		Name:        name,
		Description: "NextDNS lifecycle test",
		Arguments:   []string{stubArg, dir},
	})
	if errors.Is(err, service.ErrNotSupported) {
		t.Skip("PID 1 is not systemd")
	}
	if err != nil {
		t.Fatal(err)
	}

	dropin := "/run/systemd/system/" + unit + ".d"
	t.Cleanup(func() {
		_ = s.Stop()
		_ = s.Uninstall()
		_ = os.RemoveAll(dropin)
		_ = internal.Run("systemctl", "reset-failed", unit)
		_ = internal.Run("systemctl", "daemon-reload")
	})
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}

	// show returns a unit property, or "" if systemctl fails. Helpers below
	// do not call t.Fatal so they can be shared with subtests.
	show := func(prop string) string {
		out, err := exec.Command("systemctl", "show", "-p", prop, "--value", unit).Output()
		if err != nil {
			t.Logf("systemctl show %s: %v", prop, err)
		}
		return strings.TrimSpace(string(out))
	}
	starts := func() int {
		b, _ := os.ReadFile(filepath.Join(dir, "starts"))
		return strings.Count(string(b), "\n")
	}
	nrestarts := func() int {
		n, err := strconv.Atoi(show("NRestarts"))
		if err != nil {
			return -1
		}
		return n
	}
	waitFor := func(t *testing.T, what string, timeout time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timeout waiting for %s (ActiveState=%s SubState=%s Result=%s NRestarts=%s starts=%d)",
					what, show("ActiveState"), show("SubState"), show("Result"), show("NRestarts"), starts())
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	touch := func(t *testing.T, name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	active := func() bool { return show("ActiveState") == "active" }

	// Properties of the generated unit as loaded by systemd.
	for prop, want := range map[string]string{
		"Restart":                "on-failure",
		"RestartUSec":            "2min",
		"StartLimitBurst":        "10",
		"StartLimitIntervalUSec": "5s",
	} {
		if got := show(prop); got != want {
			t.Errorf("%s = %q, want %q", prop, got, want)
		}
	}

	// Shorten RestartSec so the test does not wait 2 minutes per restart.
	// Restart= and the start limits still come from the generated unit.
	if err := os.MkdirAll(dropin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropin, "restartsec.conf"), []byte("[Service]\nRestartSec=200ms\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := internal.Run("systemctl", "daemon-reload"); err != nil {
		t.Fatal(err)
	}
	if got := show("RestartUSec"); got != "200ms" {
		t.Fatalf("RestartUSec = %q after drop-in, want 200ms", got)
	}

	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "initial start", 5*time.Second, func() bool { return active() && starts() == 1 })

	t.Run("crash exit code is restarted", func(t *testing.T) {
		n, pid, before := nrestarts(), show("MainPID"), starts()
		touch(t, "crash")
		waitFor(t, "restart after exit 2", 5*time.Second, func() bool {
			return starts() == before+1 && nrestarts() == n+1 && active() && show("MainPID") != pid
		})
		if st, err := s.Status(); st != service.StatusRunning || err != nil {
			t.Fatalf("Status() = %v, %v; want running", st, err)
		}
	})

	t.Run("SIGKILL is restarted", func(t *testing.T) {
		n, pid, before := nrestarts(), show("MainPID"), starts()
		if err := internal.Run("systemctl", "kill", "--signal=SIGKILL", unit); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "restart after SIGKILL", 5*time.Second, func() bool {
			return starts() == before+1 && nrestarts() == n+1 && active() && show("MainPID") != pid
		})
	})

	t.Run("clean exit is not restarted", func(t *testing.T) {
		before := starts()
		touch(t, "exit0")
		waitFor(t, "clean exit", 5*time.Second, func() bool { return show("ActiveState") == "inactive" })
		time.Sleep(2 * time.Second) // 10x RestartSec
		if got := show("ActiveState"); got != "inactive" {
			t.Fatalf("ActiveState = %s after clean exit, want inactive", got)
		}
		if starts() != before {
			t.Fatalf("service restarted after clean exit (starts %d -> %d)", before, starts())
		}
		if err := s.Start(); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "start", 5*time.Second, func() bool { return active() && starts() == before+1 })
	})

	t.Run("intentional stop is not restarted", func(t *testing.T) {
		before := starts()
		if err := s.Stop(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second) // 10x RestartSec
		if got := show("ActiveState"); got != "inactive" {
			t.Fatalf("ActiveState = %s after stop, want inactive", got)
		}
		if starts() != before {
			t.Fatalf("service restarted after stop (starts %d -> %d)", before, starts())
		}
		if st, err := s.Status(); st != service.StatusStopped || err != nil {
			t.Fatalf("Status() = %v, %v; want stopped", st, err)
		}
	})

	t.Run("start limit stops a crash loop", func(t *testing.T) {
		time.Sleep(6 * time.Second) // let the 5s start limit window expire
		touch(t, "crashloop")
		before := starts()
		if err := s.Start(); err != nil {
			t.Fatal(err)
		}
		// systemd logs "Start request repeated too quickly" and leaves the
		// unit failed. Depending on the version, Result is "start-limit-hit"
		// or the result of the last failed run ("exit-code").
		waitFor(t, "failed state", 10*time.Second, func() bool { return show("ActiveState") == "failed" })
		n := starts() - before
		time.Sleep(2 * time.Second) // 10x RestartSec
		if got := starts() - before; got != n || show("ActiveState") != "failed" {
			t.Fatalf("service kept restarting after start limit (starts %d -> %d, ActiveState=%s)", n, got, show("ActiveState"))
		}
		if n < 2 || n > 10 {
			t.Fatalf("got %d starts before giving up, want between 2 and StartLimitBurst=10", n)
		}
		t.Logf("crash loop stopped after %d starts (Result=%s, NRestarts=%s)", n, show("Result"), show("NRestarts"))
		if _, err := s.Status(); err == nil || !strings.Contains(err.Error(), "failed state") {
			t.Fatalf("Status() error = %v, want failed state", err)
		}
	})
}
