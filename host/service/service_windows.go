package service

import (
	"golang.org/x/sys/windows/svc"
)

func CurrentRunMode() RunMode {
	if interactive, err = svc.IsAnInteractiveSession(); interactive || err != nil {
		return RunModeNode
	}
	return RunModeService
}
