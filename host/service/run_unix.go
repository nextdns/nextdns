//go:build !windows
// +build !windows

package service

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

func runService(name string, r Runner) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig)
	defer signal.Stop(sig)

	if err := start(r, syscall.SIGTERM); err != nil {
		return err
	}

	for {
		s := <-sig
		switch s {
		case syscall.SIGTERM:
			r.Log(fmt.Sprintf("Received signal: %s", s))
			return r.Stop()
		case syscall.SIGQUIT:
			buf := make([]byte, 100*1024)
			n := runtime.Stack(buf, true)
			r.Log(string(buf[:n]))
		case syscall.SIGCHLD, syscall.SIGURG:
			// ignore no log
		default:
			r.Log(fmt.Sprintf("Received signal: %s (ignored)", s))
		}
	}
}
