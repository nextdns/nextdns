package service

import (
	"golang.org/x/sys/windows/svc"
)

func CurrentRunMode() RunMode {
	if service, _ := svc.IsWindowsService(); service {
		return RunModeService
	}
	return RunModeNone
}
