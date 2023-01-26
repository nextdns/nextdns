package host

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
)

type windowsLogger struct {
	log debug.Log
}

func NewConsoleLogger(name string) Logger {
	return windowsLogger{log: debug.New(name)}
}

func newServiceLogger(name string) (Logger, error) {
	err := eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return nil, err
		}
	}
	el, err := eventlog.Open(name)
	if err != nil {
		return nil, err
	}
	return windowsLogger{log: el}, nil
}

func (l windowsLogger) Debug(v ...interface{}) {
	_ = l.log.Info(1, fmt.Sprint(v...))
}

func (l windowsLogger) Debugf(format string, a ...interface{}) {
	_ = l.log.Info(1, fmt.Sprintf(format, a...))
}

func (l windowsLogger) Info(v ...interface{}) {
	_ = l.log.Info(1, fmt.Sprint(v...))
}

func (l windowsLogger) Infof(format string, a ...interface{}) {
	_ = l.log.Info(1, fmt.Sprintf(format, a...))
}

func (l windowsLogger) Warning(v ...interface{}) {
	l.log.Warning(2, fmt.Sprint(v...))
}

func (l windowsLogger) Warningf(format string, a ...interface{}) {
	l.log.Warning(2, fmt.Sprintf(format, a...))
}

func (l windowsLogger) Error(v ...interface{}) {
	l.log.Error(3, fmt.Sprint(v...))
}

func (l windowsLogger) Errorf(format string, a ...interface{}) {
	l.log.Error(3, fmt.Sprintf(format, a...))
}
