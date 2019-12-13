// +build windows

package config

import (
	"os"
	"path/filepath"
)

func defaultConfPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "nextdns.conf")
	}
	return ""
}
