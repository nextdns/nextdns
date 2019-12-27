// +build !windows

package service

import (
	"os"
)

func CurrentRunMode() RunMode {
	if os.Getenv(RunModeEnv) == "1" {
		return RunModeService
	}
	return RunModeNone
}
