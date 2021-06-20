package procd

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var errUCIEntryNotFound = errors.New("entry not found")

func uci(args ...string) (string, error) {
	cmd := exec.Command("uci", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errDesc := stderr.String()
		if strings.Contains(errDesc, "uci: Entry not found") {
			return "", fmt.Errorf("uci %s: %w", strings.Join(args, " "), errUCIEntryNotFound)
		}
		return "", fmt.Errorf("uci %s: %w: %s", strings.Join(args, " "), err, errDesc)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func uciValue(value string) string {
	switch value {
	case "true":
		value = "1"
	case "false":
		value = "0"
	}
	return value
}
