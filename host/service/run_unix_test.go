//go:build !windows

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
