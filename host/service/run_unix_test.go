//go:build unix

package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

type startupSignalRunner struct{}

func (startupSignalRunner) Start() error { return fmt.Errorf("StartContext was not used") }
func (startupSignalRunner) StartContext(ctx context.Context) error {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	<-ctx.Done()
	fmt.Println("startup cancelled")
	return nil
}
func (startupSignalRunner) Stop() error { fmt.Println("stopped"); return nil }
func (startupSignalRunner) Log(string)  {}

func TestStartupSignalCancellation(t *testing.T) {
	if mode := os.Getenv("NEXTDNS_TEST_STARTUP_SIGNAL"); mode != "" {
		var err error
		if mode == "foreground" {
			err = runForeground(startupSignalRunner{})
		} else {
			err = runService("test", startupSignalRunner{})
		}
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, mode := range []string{"foreground", "service"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartupSignalCancellation$")
			cmd.Env = append(os.Environ(), "NEXTDNS_TEST_STARTUP_SIGNAL="+mode)
			out, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "startup cancelled\nstopped") {
				t.Fatalf("err=%v output=%s", err, out)
			}
		})
	}
}

// noisyStartupRunner ignores ctx during startup, like proxySvc, and gets
// another signal before SIGTERM so the catch-all channel is already full.
type noisyStartupRunner struct{}

func (noisyStartupRunner) Start() error { return fmt.Errorf("StartContext was not used") }
func (noisyStartupRunner) StartContext(context.Context) error {
	_ = syscall.Kill(os.Getpid(), syscall.SIGCHLD)
	time.Sleep(100 * time.Millisecond)
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	time.Sleep(100 * time.Millisecond)
	return nil
}
func (noisyStartupRunner) Stop() error { fmt.Println("stopped"); return nil }
func (noisyStartupRunner) Log(string)  {}

func TestStartupSignalAfterNoise(t *testing.T) {
	if os.Getenv("NEXTDNS_TEST_STARTUP_NOISE") != "" {
		if err := runService("test", noisyStartupRunner{}); err != nil {
			t.Fatal(err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartupSignalAfterNoise$")
	cmd.Env = append(os.Environ(), "NEXTDNS_TEST_STARTUP_NOISE=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "stopped") {
		t.Fatalf("SIGTERM lost during startup: err=%v output=%s", err, out)
	}
}
