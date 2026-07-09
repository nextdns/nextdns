package main

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/nextdns/nextdns/host"
)

// TestStartDoesNotBlockOnSlowOnStarted proves the startup wedge and its fix.
//
// activate() and router.Setup() run as OnStarted callbacks. They do unbounded
// blocking I/O (networksetup/netsh/systemctl/uci, systemd-resolved D-Bus). If
// one hangs, Start() must still return: it is called synchronously by
// runService (host/service/run_unix.go) BEFORE the signal-handling loop runs,
// so a Start() that never returns leaves the daemon printing "Activating"
// forever AND unable to handle SIGTERM -- systemctl stop/restart then hangs
// until SIGKILL.
//
// synctest gives a fake clock so start()'s 5s listener grace costs no real time
// and the assertion is deterministic rather than racing a wall clock.
func TestStartDoesNotBlockOnSlowOnStarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		p := &proxySvc{
			log: host.NewConsoleLogger("test"),
			OnStarted: []func(){
				func() { <-release }, // stand-in for a hung SetDNS / router Setup
			},
		}

		done := make(chan struct{})
		go func() {
			_ = p.Start()
			close(done)
		}()

		// Under synctest the clock only advances when goroutines block on time,
		// so this select fake-fires start()'s 5s grace, then either observes
		// Start() return (fixed) or trips the sentinel timeout (wedged). Both
		// timers are virtual: the test spends no real time here.
		wedged := false
		select {
		case <-done:
		case <-time.After(30 * time.Second): // sentinel >> start()'s 5s grace
			wedged = true
		}

		// Tear down regardless of outcome: release the callback so a wedged
		// Start() can finish, then stop the proxy to cancel the listener ctx --
		// otherwise synctest panics on leftover blocked goroutines.
		close(release)
		<-done
		p.Stop()

		if wedged {
			t.Fatal("Start() blocked on a slow OnStarted callback: startup wedges and the daemon cannot handle signals until SIGKILL")
		}
	})
}
