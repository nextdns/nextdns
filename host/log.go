package host

import (
	"github.com/nextdns/nextdns/host/service"
)

type Logger interface {
	Info(v ...interface{})
	Infof(format string, a ...interface{})
	Warning(v ...interface{})
	Warningf(format string, a ...interface{})
	Error(v ...interface{})
	Errorf(format string, a ...interface{})
}

func NewLogger(name string) (Logger, error) {
	if service.CurrentRunMode() == service.RunModeService {
		return newServiceLogger(name)
	}
	return NewConsoleLogger(name), nil
}
