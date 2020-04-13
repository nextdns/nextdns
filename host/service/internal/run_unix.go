package internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	stdout, err := cmd.Output()
	if err != nil {
		cancel()
		return "", fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), err)
	}
	return string(bytes.TrimSpace(stdout)), nil
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
