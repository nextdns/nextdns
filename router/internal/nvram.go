package internal

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func NVRAM(names ...string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out, err := nvram("show")
	if err != nil {
		return nil, err
	}
	s := bufio.NewScanner(strings.NewReader(out))
	names = append([]string{}, names...)
	for i := range names {
		names[i] += "="
	}
	var vars []string
	for s.Scan() {
		v := s.Text()
		for _, n := range names {
			if strings.HasPrefix(v, n) {
				vars = append(vars, v)
			}
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return vars, nil
}

func SetNVRAM(vars ...string) error {
	if len(vars) == 0 {
		return nil
	}
	for _, v := range vars {
		cmd := "set"
		if strings.HasSuffix(v, "=") {
			cmd = "unset"
		}
		if _, err := nvram(cmd, v); err != nil {
			return err
		}
	}
	_, err := nvram("commit")
	return err
}

func nvram(args ...string) (string, error) {
	cmd := exec.Command("nvram", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errDesc := stderr.String()
		return "", fmt.Errorf("nvram %s: %w: %s", strings.Join(args, " "), err, errDesc)
	}
	return strings.TrimSpace(stdout.String()), nil
}
