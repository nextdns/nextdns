package host

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strings"
)

func Model() string {
	model := tryExec([][]string{
		{"ubnt-device-info", "model"},
	})
	if model != "" {
		return model
	}
	if os := osName(); os != "" {
		return os
	}
	if version := tryExec([][]string{{"sysctl", "-n", "kernel.osrelease"}}); version != "" {
		return "Linux " + version
	}
	return "Linux"
}

func tryExec(cmds [][]string) string {
	for _, cmd := range cmds {
		if out, err := exec.Command(cmd[0], cmd[1:]...).Output(); err == nil && len(out) > 0 {
			return string(bytes.TrimSpace(out))
		}
	}
	return ""
}

func osName() string {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "PRETTY_NAME=") {
			return string(bytes.Trim(bytes.TrimSpace(scanner.Bytes()[12:]), "\""))
		}
	}
	return ""
}
