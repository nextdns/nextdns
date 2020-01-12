package launchd

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func launchctl(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	stderrStr := stderr.String()
	// launchctl can fail with a zero exit status, so check for emtpy stderr
	if stderrStr != "" && !strings.HasSuffix(stderrStr, "Operation now in progress") {
		return "", fmt.Errorf("launchctl %s: %s", strings.Join(args, " "), stderrStr)
	}

	return stdout.String(), nil
}
