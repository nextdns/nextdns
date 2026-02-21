package host

import (
	"github.com/nextdns/nextdns/host/service"
)

type Logger interface {
	Debug(v ...any)
	Debugf(format string, a ...any)
	Info(v ...any)
	Infof(format string, a ...any)
	Warning(v ...any)
	Warningf(format string, a ...any)
	Error(v ...any)
	Errorf(format string, a ...any)
}

func NewLogger(name string) (Logger, error) {
	if service.CurrentRunMode() == service.RunModeService {
		return newServiceLogger(name)
	}
	return NewConsoleLogger(name), nil
}
