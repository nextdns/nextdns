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
			r.Log(fmt.Sprintf("Recieved signal: %s", s))
			return r.Stop()
		default:
			r.Log(fmt.Sprintf("Recieved signal: %s (ignored)", s))
		}
	}
}
