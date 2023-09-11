package internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func Run(command string, arguments ...string) error {
	_, err := RunOutput(command, arguments...)
	return err
}

func RunOutput(command string, args ...string) (out string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	var stdout, stderr bytes.Buffer
	defer func() {
		cancel()
		if err != nil {
			err = fmt.Errorf("%s %s: %w: %s", command, strings.Join(args, " "), err, stderr.String())
		}
	}()
	cmd := exec.CommandContext(ctx, command, args...)
	// Obtain a pipe connected to our cmd stdout so we can read from it later.
	// cmd.Output() would block here because the underlying pipes are not closed
	// and it will try to read from them indefinitely
	var stdoutPipe, stderrPipe io.ReadCloser
	if stdoutPipe, err = cmd.StdoutPipe(); err != nil {
		err = fmt.Errorf("cannot connect stdout: %w", err)
		return
	}
	if stderrPipe, err = cmd.StderrPipe(); err != nil {
		err = fmt.Errorf("cannot connect stderr: %w", err)
		return
	}
	if err = cmd.Start(); err != nil {
		return
	}
	go copy(&stdout, stdoutPipe)
	go copy(&stderr, stderrPipe)
	err = cmd.Wait()
	out = strings.TrimSpace(stdout.String())
	return
}

func copy(w io.Writer, r io.ReadCloser) {
	_, _ = io.Copy(w, r)
}
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	for ; err != nil; err = errors.Unwrap(err) {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus()
			}
		}
	}
	return -1
}
