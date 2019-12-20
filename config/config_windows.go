// +build windows

package config

import (
	"os"
	"path/filepath"
)

func DefaultConfPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "nextdns.conf")
	}
	return ""
}
