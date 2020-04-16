package internal

import (
	"bufio"
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
	var stdout string
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	// Obtain a pipe connected to our cmd stdout so we can read from it later.
	// cmd.Output() would block here because the underlying pipes are not closed
	// and it will try to read from them indefinitely
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), err)
	}
	if err = cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), err)
	}
	go func(stdout string, stdoutPipe io.ReadCloser) {
		buf := bufio.NewReader(stdoutPipe)
		for {
			line, _ := buf.ReadString('\n')
			if len(line) > 0 {
				stdout = stdout + line + "\n"
			}
		}
	}(stdout, stdoutPipe)
	err = cmd.Wait()
	if err != nil {
		cancel()
		return "", fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), err)
	}
	return stdout, nil
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
