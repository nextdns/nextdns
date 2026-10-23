package service

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

type Runner interface {
	Start() error
	Stop() error
	Log(msg string)
}

func Run(name string, r Runner) error {
	if CurrentRunMode() == RunModeNone {
		return runForeground(r)
	}
	return runService(name, r)
}

// start lets runners with cancellable startup stop waiting for readiness when
// termination is requested. Callers also buffer the signal for normal teardown.
func start(r Runner, signals ...os.Signal) error {
	if r, ok := r.(interface{ StartContext(context.Context) error }); ok {
		ctx, cancel := signal.NotifyContext(context.Background(), signals...)
		defer cancel()
		return r.StartContext(ctx)
	}
	return r.Start()
}

func runForeground(r Runner) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(sig)
	if err := start(r, syscall.SIGHUP, syscall.SIGTERM, os.Interrupt); err != nil {
		return err
	}
	<-sig
	return r.Stop()
}
