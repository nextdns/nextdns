// +build !windows

package service

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func runService(name string, r Runner) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig)

	if err := r.Start(); err != nil {
		return err
	}

	for {
		s := <-sig
		switch s {
		case syscall.SIGTERM:
			r.Log(fmt.Sprintf("Received signal: %s", s))
			return r.Stop()
		case syscall.SIGCHLD:
			// ignore no log
		default:
			r.Log(fmt.Sprintf("Received signal: %s (ignored)", s))
		}
	}
}
