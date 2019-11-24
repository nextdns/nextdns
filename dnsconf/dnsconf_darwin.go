package dnsconf

import (
	"bytes"
	"os/exec"
)

func Get() ([]string, error) {
	b, err := exec.Command("ipconfig", "getoption", "", "domain_name_server").Output()
	if err != nil {
		return nil, err
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, ErrNotFound
	}
	return []string{string(b)}, nil
}
