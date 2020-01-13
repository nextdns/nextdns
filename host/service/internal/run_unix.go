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

func RunOutput(command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("%s: cannot connect stdout: %w", command, err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("%s: cannot connect stderr: %w", command, err)
	}
	var stdout, stderr bytes.Buffer
	go copy(&stdout, stdoutPipe)
	go copy(&stderr, stderrPipe)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%q failed: %w", command, err)
	}
	if err := cmd.Wait(); err != nil {
		cancel()
		return "", fmt.Errorf("%s %s: %w: %s", command, strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
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
